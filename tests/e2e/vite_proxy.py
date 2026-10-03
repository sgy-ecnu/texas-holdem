#!/usr/bin/env python3
"""真实 Vite dev proxy → 真实 Go 的联调检查。

断言（全部真实 HTTP，不使用 mock 配置）：
- 后端与 Vite 地址均由冻结配置 CLI 派生（受 POKER_CONFIG_FILE 影响）。
- 经 Vite proxy 的 GET /api/v1/health 返回 200。
- 直连后端、携带前端 Origin 的 POST 被 403 拒绝（证明 Origin 必须被改写）。
- 经 Vite proxy 的同一 POST 被改写 Host/Origin 后越过安全边界，落到输入校验（400）。
- 经 Vite 访问 / 返回 HTML。

只启动/停止本进程拉起的 Go 与 Vite；不碰用户依赖；退出必释放端口。
"""
import importlib.util
import os
from pathlib import Path
import signal
import socket
import subprocess
import sys
import time
import urllib.error
import urllib.request

ROOT = Path(__file__).resolve().parents[2]
_spec = importlib.util.spec_from_file_location("local", ROOT / "scripts" / "local.py")
assert _spec is not None and _spec.loader is not None
local = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(local)
require = local.require
SafeError = local.SafeError
OPENER = urllib.request.build_opener(urllib.request.ProxyHandler({}))


def port_open(port):
    try:
        with socket.create_connection(("127.0.0.1", port), timeout=1):
            return True
    except OSError:
        return False


def stop_group(proc):
    if proc is None or proc.poll() is not None:
        return
    for sig in (signal.SIGINT, signal.SIGTERM, signal.SIGKILL):
        try:
            os.killpg(os.getpgid(proc.pid), sig)
        except (ProcessLookupError, PermissionError):
            return
        try:
            proc.wait(timeout=15)
            return
        except subprocess.TimeoutExpired:
            continue


def request(method, url, *, expected, data=None, origin=None, label="request", accept="application/json"):
    headers = {"Accept": accept}
    if data is not None:
        headers["Content-Type"] = "application/json"
    if origin:
        headers["Origin"] = origin
    req = urllib.request.Request(url, data=data, method=method, headers=headers)
    try:
        response = OPENER.open(req, timeout=15)
    except urllib.error.HTTPError as error:
        response = error
    with response:
        if response.status != expected:
            raise SafeError("VITE_PROXY_HTTP_STATUS_MISMATCH:" + label +
                            ":expected=" + str(expected) + ":actual=" + str(response.status))
        return response.read(1 << 20), response.headers.get("Content-Type", "")


def wait_health(base, deadline, code):
    """轮询等待某地址的 health 就绪；容忍启动竞态，但超时即失败。"""
    end = time.monotonic() + deadline
    while time.monotonic() < end:
        try:
            body, _ = request("GET", base + "/api/v1/health", expected=200, label="health")
            if b"mysql" in body:
                return
        except (OSError, urllib.error.URLError, SafeError):
            time.sleep(0.25)
    raise SafeError(code)


def main():
    config = local.config_public()
    host = config["http_host"]
    backend = config["http_base_url"]
    backend_port = config["http_port"]
    frontend_port = config["frontend_port"]
    frontend_base = "http://" + host + ":" + str(frontend_port)
    require(not port_open(backend_port), "VITE_PROXY_BACKEND_PORT_IN_USE_STOP_NO_KILL")
    require(not port_open(frontend_port), "VITE_PROXY_FRONTEND_PORT_IN_USE_STOP_NO_KILL")

    go = subprocess.Popen(
        [sys.executable, str(ROOT / "scripts/local.py"), "dev"],
        cwd=ROOT, env=local.config_cli_env(),
        stdout=subprocess.DEVNULL, stderr=subprocess.STDOUT, start_new_session=True,
    )
    vite = None
    try:
        wait_health(backend, 90, "VITE_PROXY_BACKEND_NOT_READY")
        vite = subprocess.Popen(
            ["npm", "--prefix", "web", "run", "dev"],
            cwd=ROOT, env=local.config_cli_env(),
            stdout=subprocess.DEVNULL, stderr=subprocess.STDOUT, start_new_session=True,
        )
        deadline = time.monotonic() + 90
        while time.monotonic() < deadline:
            if vite.poll() is not None:
                raise SafeError("VITE_DEV_START_FAILED")
            if port_open(frontend_port):
                break
            time.sleep(0.25)
        require(port_open(frontend_port), "VITE_DEV_NOT_READY")

        # 1) 轮询 proxy 透传 GET health（容忍 dev server 启动竞态）
        wait_health(frontend_base, 60, "VITE_PROXY_NOT_READY")
        # 2) 直连后端带前端 Origin → 403（证明必须改写 Origin）
        _, _ = request("POST", backend + "/api/v1/games", expected=403,
                       data=b"{}", origin=frontend_base, label="direct-origin")
        # 3) 经 proxy 同请求 → 400（Host/Origin 被改写后越过安全边界，落到输入校验）
        _, _ = request("POST", frontend_base + "/api/v1/games", expected=400,
                       data=b"{}", origin=frontend_base, label="proxied-origin")
        # 4) 经 Vite 取 HTML
        body, _ = request("GET", frontend_base + "/", expected=200, label="index", accept="text/html")
        require(b"<html" in body.lower() or b"<!doctype" in body.lower(), "VITE_INDEX_NOT_HTML")

        print("vite-proxy: backend=" + backend + "; frontend=" + frontend_base +
              "; health=200; direct-origin=403; proxied-origin=400; index=html")
        print("vite-proxy: checks=4 passed=4 failed=0")
    finally:
        stop_group(vite)
        stop_group(go)
        deadline = time.monotonic() + 20
        while time.monotonic() < deadline and (port_open(backend_port) or port_open(frontend_port)):
            time.sleep(0.3)
        released = not (port_open(backend_port) or port_open(frontend_port))
        print("vite-proxy: own processes stopped; ports-released=" + str(released))
        require(released, "VITE_PROXY_PORT_NOT_RELEASED")
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except SafeError as error:
        print(str(error), file=sys.stderr)
        raise SystemExit(1)
    except KeyboardInterrupt:
        raise SystemExit(130)
    except Exception:
        print("VITE_PROXY_FAILED_REDACTED", file=sys.stderr)
        raise SystemExit(1)
