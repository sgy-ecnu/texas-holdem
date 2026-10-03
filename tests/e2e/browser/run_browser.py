#!/usr/bin/env python3
"""真实浏览器验收 wrapper。

职责边界：
- 只负责启动/停止本轮自己拉起的真实 Go 子进程（经 scripts/local.py dev，环境由 local.py 从受保护文件安全载入）。
- 绝不读取、打印或转发任何凭据；子进程输出被 local.py 过滤，本 wrapper 再不回传。
- 依赖（MySQL 3307 / Redis 6380）保留不停止；结束必释放 8081。
- 不写入 infra 的 run.py / Makefile。
用法：
  python3 tests/e2e/browser/run_browser.py            # 全部场景
  python3 tests/e2e/browser/run_browser.py --grep 场景4 # 透传给 playwright test
"""
import importlib.util
import json
import os
from pathlib import Path
import re
import signal
import subprocess
import sys
import time
import urllib.error
import urllib.request

ROOT = Path(__file__).resolve().parents[3]
BROWSER_DIR = Path(__file__).resolve().parent

_spec = importlib.util.spec_from_file_location("local", ROOT / "scripts" / "local.py")
assert _spec is not None and _spec.loader is not None
local = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(local)
require = local.require
SafeError = local.SafeError

HTTP = urllib.request.build_opener(urllib.request.ProxyHandler({}))
# 服务地址唯一来源为冻结配置 CLI；无 CLI 即阻塞，不使用硬编码端口。
_CONFIG = None


def resolved_config():
    global _CONFIG
    if _CONFIG is None:
        _CONFIG = local.config_public()
    return _CONFIG


def base_url():
    return resolved_config()["http_base_url"]


def http_port():
    return resolved_config()["http_port"]


def pids_on(port):
    p = subprocess.run(["lsof", "-nP", "-iTCP:%d" % port, "-sTCP:LISTEN", "-t"],
                       capture_output=True, text=True)
    return [int(x) for x in p.stdout.split() if x.strip().isdigit()]


def pgid_of(pid):
    p = subprocess.run(["ps", "-o", "pgid=", "-p", str(pid)], capture_output=True, text=True)
    return int(p.stdout.strip()) if p.stdout.strip().isdigit() else None


def cmd_of(pid):
    p = subprocess.run(["ps", "-o", "command=", "-p", str(pid)], capture_output=True, text=True)
    return p.stdout.strip()


def health():
    try:
        with HTTP.open(base_url() + "/api/v1/health", timeout=3) as response:
            return json.load(response)
    except (OSError, urllib.error.URLError, ValueError):
        return None


def start_service():
    require((ROOT / "web/dist/index.html").is_file(), "FRONTEND_DIST_MISSING")
    require(not pids_on(http_port()), "HTTP_PORT_IN_USE_STOP_NO_KILL")
    proc = subprocess.Popen(
        [sys.executable, str(ROOT / "scripts/local.py"), "dev"],
        # 透传白名单（含 POKER_CONFIG_FILE），凭据由 local.py 自行从受保护文件读取。
        cwd=ROOT, env=local.config_cli_env(),
        stdout=subprocess.DEVNULL, stderr=subprocess.STDOUT,
        start_new_session=True,
    )
    deadline = time.monotonic() + 120
    while time.monotonic() < deadline:
        if proc.poll() is not None:
            raise SafeError("SERVER_START_FAILED_LOGS_REDACTED")
        state = health()
        if state and state.get("allow_paid_models") is False:
            return proc
        time.sleep(0.5)
    stop_service(proc)
    raise SafeError("SERVER_START_TIMEOUT")


def stop_service(proc):
    if proc.poll() is None:
        try:
            os.killpg(os.getpgid(proc.pid), signal.SIGINT)
        except (ProcessLookupError, PermissionError):
            pass
        try:
            proc.wait(timeout=30)
        except subprocess.TimeoutExpired:
            try:
                os.killpg(os.getpgid(proc.pid), signal.SIGTERM)
            except (ProcessLookupError, PermissionError):
                pass
            try:
                proc.wait(timeout=10)
            except subprocess.TimeoutExpired:
                try:
                    os.killpg(os.getpgid(proc.pid), signal.SIGKILL)
                except (ProcessLookupError, PermissionError):
                    pass
    # local.py 的 go run 自成会话，额外按 8081 监听组兜底回收（仅限本项目进程）。
    for _ in range(40):
        pids = pids_on(http_port())
        if not pids:
            break
        removed = False
        for pid in pids:
            command = cmd_of(pid)
            if any(marker in command for marker in ("cmd/server", "go-build", "texas-holdem")):
                group = pgid_of(pid)
                if group:
                    try:
                        os.killpg(group, signal.SIGKILL)
                        removed = True
                    except (ProcessLookupError, PermissionError):
                        pass
        if not removed:
            break
        time.sleep(0.3)
    require(not pids_on(http_port()), "SERVER_STOP_TIMEOUT")


def run_playwright(extra):
    stamp = time.strftime("%Y%m%d-%H%M%S")
    screen_dir = ROOT / "web/node_modules/.cache/browser-verify" / stamp
    screen_dir.mkdir(parents=True, exist_ok=True)
    env = dict(os.environ)
    env["SCREEN_DIR"] = str(screen_dir)
    env["POKER_BASE_URL"] = base_url()  # 与 resolver 同源，绝不让测试指向与服务不同的地址
    env["PYTHONDONTWRITEBYTECODE"] = "1"
    cmd = ["npx", "playwright", "test", *extra]
    proc = subprocess.run(cmd, cwd=BROWSER_DIR, env=env, capture_output=True, text=True, timeout=1200)
    print(proc.stdout, end="")
    if proc.stderr.strip():
        print(proc.stderr, end="")
    match = re.search(r"(\d+) passed", proc.stdout)
    passed = int(match.group(1)) if match else 0
    match = re.search(r"(\d+) failed", proc.stdout)
    failed = int(match.group(1)) if match else 0
    print("browser-wrapper: passed=%d failed=%d exit=%d" % (passed, failed, proc.returncode))
    print("browser-wrapper: screenshots=" + str(screen_dir))
    print("browser-wrapper: note=infra run.py 'browser' 仍为占位，本目录为独立可复现入口")
    return proc.returncode


def main():
    proc = start_service()
    try:
        code = run_playwright(sys.argv[1:])
    finally:
        stop_service(proc)
        print("browser-wrapper: own HTTP process stopped; dependencies retained")
    return code


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except SafeError as error:
        print(str(error), file=sys.stderr)
        raise SystemExit(1)
    except KeyboardInterrupt:
        raise SystemExit(130)
    except Exception:
        print("BROWSER_WRAPPER_FAILED_REDACTED", file=sys.stderr)
        raise SystemExit(1)
