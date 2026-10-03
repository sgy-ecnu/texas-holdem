#!/usr/bin/env python3
"""真实 Go 二进制 + 本地 MySQL/Redis + HTTP；无模型网络调用。"""
import importlib.util
import json
import os
from pathlib import Path
import re
import select
import signal
import socket
import subprocess
import sys
import tempfile
import threading
import time
import traceback
from typing import Any
import urllib.error
import urllib.request

ROOT = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location("local", ROOT / "scripts/local.py")
assert spec is not None and spec.loader is not None
local = importlib.util.module_from_spec(spec)
spec.loader.exec_module(local)
require = local.require
HTTP = urllib.request.build_opener(urllib.request.ProxyHandler({}))
# 服务地址唯一来源为冻结配置 CLI；无 CLI 即阻塞，不使用硬编码端口。
_RESOLVED = None


def resolved_config():
    global _RESOLVED
    if _RESOLVED is None:
        _RESOLVED = local.config_public()
    return _RESOLVED


def base_url() -> str:
    return resolved_config()["http_base_url"]


def request(path, data=None, expected=200, raw=False, headers=None) -> Any:
    payload = json.dumps(data).encode() if data is not None else None
    req = urllib.request.Request(base_url() + path, data=payload,
                                 headers={"Content-Type": "application/json", **(headers or {})})
    try:
        response = HTTP.open(req, timeout=15)
    except urllib.error.HTTPError as error:
        response = error
    with response:
        require(response.status == expected, "HTTP_STATUS_MISMATCH")
        body = response.read(4 << 20)
        return body if raw else json.loads(body)


def wait_for(fn, code, seconds=30):
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline:
        value = fn()
        if value:
            return value
        time.sleep(0.03)
    raise local.SafeError(code)


class RedisProxy:
    """只关闭测试自身代理，绝不停止用户 Redis 或 flush 数据。"""
    def __init__(self, port):
        self.upstream_port = port
        self.listener = socket.socket()
        self.listener.bind(("127.0.0.1", 0))
        self.port = self.listener.getsockname()[1]
        self.listener.listen()
        self.listener.settimeout(0.2)
        self.closed = threading.Event()
        self.peers = []
        self.thread = threading.Thread(target=self.accept, daemon=True)
        self.thread.start()

    def accept(self):
        while not self.closed.is_set():
            try:
                client, _ = self.listener.accept()
            except socket.timeout:
                continue
            except OSError:
                return
            threading.Thread(target=self.forward, args=(client,), daemon=True).start()

    def forward(self, client):
        try:
            with client, socket.create_connection(("127.0.0.1", self.upstream_port), timeout=3) as upstream:
                self.peers.extend([client, upstream])
                while not self.closed.is_set():
                    ready, _, _ = select.select([client, upstream], [], [], 0.2)
                    for source in ready:
                        data = source.recv(65536)
                        if not data:
                            return
                        (upstream if source is client else client).sendall(data)
        except OSError:
            return  # 测试故障注入的预期网络终止；健康/HTTP 断言为最终判断边界。

    def close(self):
        self.closed.set()
        self.listener.close()
        for peer in self.peers:
            peer.close()
        self.thread.join(timeout=2)


def redis_public(storage, game_id):
    with socket.create_connection((storage["redis_host"], storage["redis_port"]), timeout=3) as sock:
        stream = sock.makefile("rb")

        def command(*parts):
            pieces = [p.encode() for p in parts]
            sock.sendall(b"*" + str(len(pieces)).encode() + b"\r\n" + b"".join(
                b"$" + str(len(p)).encode() + b"\r\n" + p + b"\r\n" for p in pieces))
            line = stream.readline(4096)
            require(line, "REDIS_RESPONSE_MISSING")
            if line[:1] == b"$":
                size = int(line[1:])
                require(0 <= size <= 4 << 20, "REDIS_PUBLIC_MISSING")
                data = stream.read(size)
                require(stream.read(2) == b"\r\n", "REDIS_PROTOCOL_INVALID")
                return data
            require(line[:1] != b"-", "REDIS_COMMAND_FAILED")
            return line

        if storage.get("redis_password"):
            command("AUTH", storage["redis_password"])
        command("SELECT", str(storage["redis_db"]))
        return json.loads(command("GET", "poker:game:" + game_id + ":public"))


class Harness:
    def __init__(self, values, storage, binary, cwd):
        self.values, self.storage, self.binary, self.cwd = values, storage, binary, cwd
        # 脱敏集合：合并 .env.local（可能为空）与 YAML 直连存储字段（含 secret）。
        self.redaction = {**values, **storage}
        self.process = None
        self.created = set()
        self.proxy = None
        self.logs = []
        self.log_thread = None

    def start(self, overrides=None):
        require(self.process is None, "TEST_PROCESS_ALREADY_RUNNING")
        values = dict(self.values)
        # 绝对 POKER_CONFIG_FILE + 只注入凭据/显式测试覆盖；E2E 强制 mock + false。
        env = local.service_env(values, e2e=True, explicit=dict(overrides or {}))
        self.process = subprocess.Popen([str(self.binary)], cwd=self.cwd,
                                        env=env,
                                        stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
        process = self.process

        def collect():
            assert process.stdout is not None
            for line in process.stdout:
                self.logs.append(local.sanitize_output(line.decode("utf-8", errors="replace"), self.redaction))

        self.log_thread = threading.Thread(target=collect, daemon=True)
        self.log_thread.start()

        def ready():
            assert self.process is not None
            require(self.process.poll() is None, "SERVER_START_FAILED_LOGS_REDACTED")
            try:
                return request("/api/v1/health")["mysql"] == "available"
            except (OSError, urllib.error.URLError):
                return False

        wait_for(ready, "SERVER_START_TIMEOUT")

    def stop(self, crash=False):
        if self.process is not None:
            if self.process.poll() is None:
                self.process.send_signal(signal.SIGKILL if crash else signal.SIGTERM)
                try:
                    self.process.wait(timeout=20)
                except subprocess.TimeoutExpired:
                    self.process.kill()
                    self.process.wait()
                    raise local.SafeError("SERVER_STOP_TIMEOUT")
            self.process = None
            if self.log_thread:
                self.log_thread.join(timeout=5)

    def create(self, hands=3):
        data = {"seats": [{"seat": n, "name": "E2E " + str(n), "model_ref": "mock", "stack": 100000000}
                          for n in range(2)], "button": 0, "small_blind": 5, "big_blind": 10,
                "max_hands": hands, "mode": "mock", "max_live_calls": 0}
        view = request("/api/v1/games", data, expected=201)
        self.created.add(view["id"])
        require(view["mode"] == "mock" and view["live_calls_used"] == 0, "MOCK_ONLY_REQUIRED")
        return view["id"]

    def get(self, game_id):
        return request("/api/v1/games/" + game_id)

    def control(self, game_id, command):
        return request("/api/v1/games/" + game_id + "/commands", {"command": command})

    def cleanup(self):
        # 仅通过公开 API 停止本轮创建的游戏；不删数据库记录或 Redis key。
        if self.process is not None and self.process.poll() is None:
            for game_id in self.created:
                view = self.get(game_id)
                if view["status"] not in {"stopped", "completed", "failed"}:
                    self.control(game_id, "stop")
        self.stop()
        if self.proxy:
            self.proxy.close()


def assert_public(value):
    private = {"hole_cards", "holecards", "deck", "snapshot", "observation", "apikey", "api_key", "password"}
    if isinstance(value, dict):
        require(not private.intersection(key.lower() for key in value), "PUBLIC_PRIVATE_FIELD_LEAK")
        for child in value.values():
            assert_public(child)
    elif isinstance(value, list):
        for child in value:
            assert_public(child)


def test_mock_multihand(h):
    game_id = h.create()
    h.control(game_id, "start")
    final = wait_for(lambda: (v if (v := h.get(game_id))["status"] == "completed" else None),
                     "MULTIHAND_TIMEOUT", seconds=60)
    require(final["hands_completed"] == 3 and final["live_calls_used"] == 0, "MULTIHAND_COUNT_INVALID")
    require(final["last_action"]["source"] == "mock", "ACTION_NOT_MOCK")
    for number in range(1, 4):
        hand = request("/api/v1/games/" + game_id + "/hands/" + str(number))
        require(hand["hand_no"] == number and sum(p["stack"] for p in hand["final_stacks"]) == 200000000,
                "HAND_PERSISTENCE_OR_CHIPS_INVALID")
    # game_id 是服务器 UUID，仍校验后才插入只读 SQL。
    require(re.fullmatch(r"[a-f0-9-]{36}", game_id), "GAME_ID_INVALID")
    result = local.mysql_query(h.storage,
                               "SELECT COUNT(*) FROM hands WHERE game_id='" + game_id + "';"
                               "SELECT COUNT(*)>0 FROM actions WHERE game_id='" + game_id + "';")
    require(result.returncode == 0 and result.stdout.split() == ["3", "1"], "MYSQL_PERSISTENCE_INVALID")


def test_public_private_isolation(h):
    game_id = h.create(10000)
    h.control(game_id, "start")
    wait_for(lambda: h.get(game_id)["hand"] is not None, "HAND_NOT_STARTED")
    for _ in range(10):
        assert_public(h.get(game_id))
        assert_public(request("/api/v1/games/current"))
        assert_public(redis_public(h.storage, game_id))
    h.control(game_id, "pause")
    h.control(game_id, "stop")


def test_pause_resume_stop(h):
    game_id = h.create(10000)
    h.control(game_id, "start")
    wait_for(lambda: h.get(game_id)["last_action"] is not None, "ACTION_NOT_STARTED")
    paused = h.control(game_id, "pause")
    time.sleep(0.25)
    require(h.get(game_id)["revision"] == paused["revision"], "PAUSE_NOT_STABLE")
    h.control(game_id, "resume")
    wait_for(lambda: h.get(game_id)["revision"] > paused["revision"] + 1, "RESUME_NOT_ADVANCING")
    stopped = h.control(game_id, "stop")
    time.sleep(0.25)
    require(stopped["status"] == "stopped" and h.get(game_id)["revision"] == stopped["revision"],
            "STOP_NOT_STABLE")


def test_restart_recovery(h):
    game_id = h.create(10000)
    h.control(game_id, "start")
    wait_for(lambda: h.get(game_id)["last_action"] is not None, "ACTION_NOT_STARTED")
    h.stop(crash=True)
    h.start()
    recovered = h.get(game_id)
    require(recovered["status"] == "paused" and recovered["reason"] == "recovery", "RECOVERY_NOT_PAUSED")
    time.sleep(0.25)
    require(h.get(game_id)["revision"] == recovered["revision"], "RECOVERY_AUTO_ADVANCED")
    h.control(game_id, "resume")
    wait_for(lambda: h.get(game_id)["revision"] > recovered["revision"] + 1, "RECOVERY_RESUME_FAILED")
    h.control(game_id, "stop")


def _reconstructed_dsn(storage):
    return (storage["mysql_user"] + ":" + storage["mysql_password"] +
            "@tcp(" + storage["mysql_host"] + ":" + str(storage["mysql_port"]) + ")/" +
            storage["mysql_database"] + "?parseTime=true")


def _temp_base_config(redis_addr):
    """临时 base YAML（无同目录 local）：仅用于 E2E 故障注入，值非 secret、不打印。"""
    directory = tempfile.mkdtemp(prefix="e2e-cfg-", dir=ROOT / "tmp")
    path = Path(directory) / "config.yaml"
    path.write_text(
        "data:\n"
        "  mysql:\n"
        "    dsn_env: POKER_MYSQL_DSN\n"
        "  redis:\n"
        "    addr: " + redis_addr + "\n"
        "    db: 0\n"
        "    password_env: POKER_REDIS_PASSWORD\n"
        "agents:\n"
        "  allow_paid_models: false\n")
    path.chmod(0o600)
    return path


def test_redis_runtime_degradation(h):
    h.stop()
    # 直连字段优先于 env，故通过临时 base 指向本测试代理端口。
    h.proxy = RedisProxy(h.storage["redis_port"])
    h.start({"POKER_CONFIG_FILE": str(_temp_base_config("127.0.0.1:" + str(h.proxy.port))),
             "POKER_MYSQL_DSN": _reconstructed_dsn(h.storage),
             "POKER_REDIS_PASSWORD": h.storage["redis_password"]})
    game_id = h.create(10000)
    h.control(game_id, "start")
    wait_for(lambda: h.get(game_id)["last_action"] is not None, "ACTION_NOT_STARTED")
    paused = h.control(game_id, "pause")
    h.proxy.close()
    health = request("/api/v1/health", expected=503)
    require(health["mysql"] == "available" and health["redis"] == "unavailable" and health["storage_degraded"],
            "REDIS_HEALTH_DEGRADATION_INVALID")
    h.control(game_id, "resume")
    view = wait_for(lambda: (v if (v := h.get(game_id))["revision"] > paused["revision"] + 1 else None),
                    "MYSQL_AUTHORITY_NOT_ADVANCING")
    require(view["storage_degraded"], "GAME_DEGRADATION_NOT_VISIBLE")
    h.control(game_id, "stop")
    h.stop()
    h.proxy = None
    local.redis_ping(h.storage)
    h.start()


def test_dependency_startup_failures(h):
    h.stop()
    # 保留一个已绑定但不 listen 的 loopback socket，避免错误端口碰到其他服务。
    with socket.socket() as blocker:
        blocker.bind(("127.0.0.1", 0))
        port = blocker.getsockname()[1]
        for dependency in ("mysql", "redis"):
            if dependency == "mysql":
                # 临时 base 无直连 dsn，故 env DSN 生效并被指向死端口。
                explicit = {"POKER_CONFIG_FILE": str(_temp_base_config("127.0.0.1:" + str(h.storage["redis_port"]))),
                            "POKER_MYSQL_DSN": "fixture_app:" + "x" * 32 +
                                               "@tcp(127.0.0.1:" + str(port) + ")/texas_holdem?parseTime=true",
                            "POKER_REDIS_PASSWORD": h.storage["redis_password"]}
            else:
                explicit = {"POKER_CONFIG_FILE": str(_temp_base_config("127.0.0.1:" + str(port))),
                            "POKER_MYSQL_DSN": _reconstructed_dsn(h.storage),
                            "POKER_REDIS_PASSWORD": h.storage["redis_password"]}
            p = subprocess.run([str(h.binary)], cwd=h.cwd,
                               env=local.service_env(h.values, e2e=True, explicit=explicit),
                               stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=30)
            require(p.returncode != 0 and b"STORAGE_UNAVAILABLE" in p.stderr,
                    "DEPENDENCY_STARTUP_DID_NOT_FAIL_WITH_STORAGE_ERROR")
            config = resolved_config()
            try:
                with socket.create_connection((config["http_host"], config["http_port"]), timeout=1):
                    raise local.SafeError("FAILED_START_EXPOSED_HTTP")
            except OSError:
                pass
            print("  dependency=" + dependency + ": expected failure; HTTP closed")
    h.start()


def test_embedded_resources(h):
    # 二进制运行于空临时 cwd，无 dist sidecar；不移动/改写前端文件。
    html = request("/", raw=True)
    require(b"<html" in html.lower() and b"<script" in html.lower(), "EMBEDDED_INDEX_INVALID")
    assets = re.findall(rb'(?:src|href)=["\'](/assets/[^"\']+)["\']', html)
    require(assets and any(path.endswith(b".js") for path in assets), "EMBEDDED_JS_MISSING")
    for path in assets:
        require(request(path.decode(), raw=True), "EMBEDDED_ASSET_EMPTY")
    require(request("/table", raw=True) == html, "SPA_FALLBACK_INVALID")


def test_http_host_origin_contract(h):
    config = resolved_config()
    host, port = config["http_host"], config["http_port"]
    own_host = host + ":" + str(port)
    request("/api/v1/health", headers={"Host": own_host})
    if host != "localhost":
        request("/api/v1/health", headers={"Host": "localhost:" + str(port)})
    for bad in (host + ":" + str(port - 1), "localhost:" + str(port - 1), host + ":" + str(port + 1)):
        request("/api/v1/health", expected=403, headers={"Host": bad})
    for origin in ("http://" + host + ":" + str(port - 1), "http://" + host + ":" + str(port + 1),
                   "http://localhost:" + str(port)):
        request("/api/v1/games", data={}, expected=403,
                headers={"Host": own_host, "Origin": origin})
    # 同源请求通过安全边界后由输入校验拒绝空 payload（400，而不是 403）。
    request("/api/v1/games", data={}, expected=400,
            headers={"Host": own_host, "Origin": config["http_base_url"]})
    print("  host-origin-contract: resolved=" + own_host + "; neighbor/other-origin Host and Origin rejected")


def test_http_aborted_polling(h):
    game_id = h.create(10000)
    h.control(game_id, "start")
    wait_for(lambda: h.get(game_id)["last_action"] is not None, "ABORT_WORKER_NOT_STARTED")
    before = h.get(game_id)["revision"]
    config = resolved_config()
    own_host = config["http_host"] + ":" + str(config["http_port"])
    paths = ["/api/v1/games/" + game_id, "/api/v1/games/current", "/api/v1/health"]
    for index in range(150):
        path = paths[index % 3]
        with socket.create_connection((config["http_host"], config["http_port"]), timeout=3) as connection:
            connection.sendall(("GET " + path + " HTTP/1.1\r\nHost: " + own_host + "\r\n"
                                "Connection: close\r\n\r\n").encode("ascii"))
            # 完整请求发送后立即断开，不读取响应；不声称必然取消 SQL。
        if index % 15 == 14:
            for probe in paths:
                request(probe)
    after = wait_for(lambda: (v if (v := h.get(game_id))["revision"] > before else None),
                     "ABORT_WORKER_DID_NOT_ADVANCE")
    require(after["id"] == game_id and after["status"] == "running", "ABORT_CHANGED_OR_FAILED_GAME")
    health = request("/api/v1/health")
    require(health["mysql"] == "available" and health["redis"] == "available", "ABORT_STORAGE_HEALTH_FAILED")
    paused = h.control(game_id, "pause")
    time.sleep(0.2)
    require(h.get(game_id)["revision"] == paused["revision"], "ABORT_PAUSE_NOT_STABLE")
    h.control(game_id, "resume")
    wait_for(lambda: h.get(game_id)["revision"] > paused["revision"] + 1, "ABORT_RESUME_NOT_ADVANCING")
    h.control(game_id, "stop")
    print("  abort-stress: disconnects=150; per-route=50; followup-http=200; same-game-worker=advancing")
    print("  abort-stress: SQL cancellation occurrence is nondeterministic; unit tests prove deterministic cancellation")


CASES = {"mock": test_mock_multihand, "privacy": test_public_private_isolation,
         "controls": test_pause_resume_stop, "recovery": test_restart_recovery,
         "redis": test_redis_runtime_degradation, "dependencies": test_dependency_startup_failures,
           "embedded": test_embedded_resources, "abort": test_http_aborted_polling,
           "port": test_http_host_origin_contract}


def main():
    name = sys.argv[1] if len(sys.argv) == 2 else "all"
    if name == "browser":
        print("browser: BLOCKED PLAYWRIGHT_PENDING_FRONTEND; tests=0 passed=0 failed=0 blocked=1")
        return 2
    require(name == "all" or name in CASES, "UNKNOWN_E2E_SCENARIO")
    cases = CASES if name == "all" else {name: CASES[name]}
    try:
        require((ROOT / "web/dist/index.html").is_file(), "FRONTEND_DIST_MISSING")
        config = local.config_public()
        values = local.load_env(e2e=True)
        storage = local.storage_config()
        redaction = {**values, **storage}
        binary = ROOT / "tmp/texas-holdem"
        require(binary.is_file(), "BUILD_REQUIRED")
        require(json.loads(local.CATALOG.read_text()) == {"provider": {}}, "MOCK_CATALOG_CHANGED")
        local.doctor(e2e=True)
        try:
            with socket.create_connection((config["http_host"], config["http_port"]), timeout=1):
                raise local.SafeError("HTTP_PORT_IN_USE_STOP_NO_KILL")
        except OSError:
            pass
        # 在启动调度器前只读检查；现有活动游戏必须由用户决定，不自动停止/恢复。
        result = local.mysql_query(storage, "SELECT COUNT(*) FROM games WHERE active_slot=1;")
        require(result.returncode == 0, "MIGRATE_REQUIRED")
        require(result.stdout.strip() == "0", "EXISTING_ACTIVE_GAME_STOP")
    except Exception as error:
        code = str(error) if isinstance(error, local.SafeError) else "PREFLIGHT_FAILED_REDACTED"
        for key, test in cases.items():
            print(key + " -> " + test.__name__ + ": BLOCKED " + code)
        print("e2e: tests=0 passed=0 failed=0 blocked=" + str(len(cases)))
        return 2
    passed = failed = 0
    (ROOT / "tmp").mkdir(exist_ok=True)
    with tempfile.TemporaryDirectory(prefix="e2e-", dir=ROOT / "tmp") as cwd:
        h = Harness(values, storage, binary, cwd)
        try:
            h.start()
            models = request("/api/v1/models")
            require([m["ref"] for m in models["models"]] == ["mock"], "NON_MOCK_CATALOG_EXPOSED")
            for key, test in cases.items():
                try:
                    test(h)
                    passed += 1
                    print(key + " -> " + test.__name__ + ": PASS")
                except Exception as error:
                    failed += 1
                    code = str(error) if isinstance(error, local.SafeError) else "CASE_FAILED_REDACTED"
                    print(key + " -> " + test.__name__ + ": FAIL " + code)
                    local.safe_output(traceback.format_exc(), redaction)
                    print("server logs (credential-redacted only):")
                    print("".join(h.logs), end="")
                    break  # 故障后不继续操作可能已改变的持久化状态。
        except Exception:
            failed += 1
            print("harness: FAIL START_OR_MODELS_CHECK_REDACTED")
            local.safe_output(traceback.format_exc(), redaction)
            print("server logs (credential-redacted only):")
            print("".join(h.logs), end="")
        finally:
            try:
                h.cleanup()
            except Exception:
                failed += 1
                h.stop(crash=True)
                if h.proxy:
                    h.proxy.close()
                print("cleanup: FAIL RECORDS_RETAINED_REDACTED")
                local.safe_output(traceback.format_exc(), redaction)
    blocked = max(0, len(cases) - passed - failed)
    print("e2e: tests=" + str(passed + failed) + " passed=" + str(passed) +
          " failed=" + str(failed) + " blocked=" + str(blocked))
    return 1 if failed else 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except Exception:
        print("E2E_OPERATION_FAILED_REDACTED; tests=0 passed=0 failed=1")
        raise SystemExit(1)
