#!/usr/bin/env python3
"""「牌桌配置模板」前端真实链路验收 wrapper（阶段 3）。

职责边界（与 run_seat_model.py / run_browser.py 同源安全约定）：
- 只启动/停止本轮自己拉起的真实 Go 子进程；Redis 密码由 local.py 在本地进程内从受保护配置
  读入并注入子进程环境，全程不打印、不回传；MySQL 用一次性 scratch 库（root 空密码，无密钥）。
- 用户在库仍是 schema v1，阶段 2 后真实服务拒绝启动；本 wrapper 不改用户库、不改仓库 config，
  只在临时目录生成一份 E2E 专用配置，指向 scratch 库；结束后删除 scratch 库。
- 依赖（MySQL 3306 / Redis 6379）保留不停止；结束必释放 8081。
- 不 commit/push。完整命令原始输出写入 docs/TABLE_TEMPLATE_FRONTEND.md。
用法：python3 tests/e2e/browser/run_table_template.py
"""
import importlib.util
import json
import os
from pathlib import Path
import re
import shlex
import signal
import socket
import subprocess
import sys
import tempfile
import textwrap
import time
import urllib.error
import urllib.request

ROOT = Path(__file__).resolve().parents[3]
BROWSER = Path(__file__).resolve().parent
REPORT = ROOT / 'docs/TABLE_TEMPLATE_FRONTEND.md'
BASE = 'http://127.0.0.1:8081'
SCRATCH_DB = 'texas_holdem_frontend_verify'

spec = importlib.util.spec_from_file_location('local', ROOT / 'scripts/local.py')
assert spec and spec.loader
local = importlib.util.module_from_spec(spec)
spec.loader.exec_module(local)
HTTP = urllib.request.build_opener(urllib.request.ProxyHandler({}))

# 运行期秘密只用于脱敏子进程输出；不打印、不写盘。
SECRETS: dict[str, str] = {}


def stage(log, argv, cwd, env, timeout=900):
    command = shlex.join(argv)
    print('$ ' + command, flush=True)
    result = subprocess.run(argv, cwd=cwd, env=env, capture_output=True, text=True, timeout=timeout)
    output = local.sanitize_output(result.stdout + result.stderr, SECRETS)
    output = re.sub(r'(?i)(poker_player=)[^;\s]+', r'\1[凭据已脱敏]', output)
    log.write('\n### 命令\n\n```sh\n' + command + '\n```\n\n工作目录：`' + str(cwd) +
              '`\n\n```text\n' + output + ('' if output.endswith('\n') else '\n') +
              '```\n\n退出码：' + str(result.returncode) + '\n')
    log.flush()
    print(output, end='' if output.endswith('\n') else '\n', flush=True)
    print('exit=' + str(result.returncode), flush=True)
    return result


def get(path):
    try:
        response = HTTP.open(BASE + path, timeout=3)
    except urllib.error.HTTPError as error:
        response = error
    with response:
        return response.status, response.read()


def port_free(port):
    with socket.socket() as s:
        return s.connect_ex(('127.0.0.1', port)) != 0


def stop(process):
    if process and process.poll() is None:
        try:
            os.killpg(os.getpgid(process.pid), signal.SIGTERM)
        except (ProcessLookupError, PermissionError):
            pass
        try:
            process.wait(timeout=20)
        except subprocess.TimeoutExpired:
            try:
                os.killpg(os.getpgid(process.pid), signal.SIGKILL)
            except (ProcessLookupError, PermissionError):
                pass
            process.wait()


def reset_scratch_db():
    """创建一次性 scratch 库；用户库 texas_holdem 完全不触碰。"""
    sql = (f'DROP DATABASE IF EXISTS {SCRATCH_DB}; '
           f'CREATE DATABASE {SCRATCH_DB} CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;')
    return local.mysql(sql, user='root', password='', database='information_schema')


def drop_scratch_db():
    return local.mysql(f'DROP DATABASE IF EXISTS {SCRATCH_DB};',
                       user='root', password='', database='information_schema')


def scratch_schema_version():
    r = local.mysql('SELECT MAX(version) FROM schema_version;', user='root', password='',
                    database=SCRATCH_DB)
    return r.returncode, (r.stdout or '').strip()


CONFIG_TEMPLATE = textwrap.dedent(
    """\
    # E2E 一次性配置（临时目录，不入 git；不含任何密钥）。
    # MySQL 指向 scratch 库；Redis 密码由 POKER_REDIS_PASSWORD 在进程内注入。
    server:
      http:
        host: 127.0.0.1
        port: 8081
        timeout: 10s
    frontend:
      dev_port: 5173
    data:
      mysql:
        dsn: root:@tcp(127.0.0.1:3306)/{db}?parseTime=true
      redis:
        addr: 127.0.0.1:6379
        db: 0
        password_env: POKER_REDIS_PASSWORD
    gameplay:
      human_action_timeout: 5s
      ai_turn_delay: 200ms
    agents:
      catalog_path: auto
      request_timeout: 15s
      max_output_tokens: 256
      allow_paid_models: false
      max_live_calls: 1000
    """
)


def main():
    failures = []
    server = None
    clean = local.child_env()
    REPORT.write_text('# 牌桌配置模板：阶段 3 前端真实链路验证证据\n\n'
                      '运行时间：' + time.strftime('%Y-%m-%d %H:%M:%S %z') + '\n\n'
                      '复现命令（项目根目录）：`python3 tests/e2e/browser/run_table_template.py`。\n'
                      '真实 Go 二进制 + 嵌入最新 web/dist + Chromium + 真实 MySQL/Redis。\n'
                      '**scratch 库说明**：用户库 `texas_holdem` 仍为 schema v1（阶段 2 需 v2），真实服务会拒绝启动。'
                      '本 wrapper 不改用户库与仓库 config，只在临时目录生成 E2E 专用配置，'
                      '指向一次性 scratch 库 `' + SCRATCH_DB + '`（root 空密码，无密钥），结束后删除。\n'
                      'Redis 密码由 local.py 在本地进程内从受保护配置读入并注入子进程，全程不打印、不回传。\n'
                      '不改后端（阶段 2 已完成）。不 commit/push。\n', encoding='utf-8')
    try:
        with REPORT.open('a', encoding='utf-8') as log:
            try:
                storage = local.storage_config()
                local.require(storage['mysql_host'] == '127.0.0.1' and storage['mysql_port'] == 3306 and
                              storage['redis_port'] == 6379, 'LOCAL_ENDPOINT_REQUIRED')
                SECRETS['POKER_REDIS_PASSWORD'] = storage['redis_password']
                local.redis_ping(storage)

                created = reset_scratch_db()
                local.require(created.returncode == 0, 'SCRATCH_DB_CREATE_FAILED')
                probe = local.mysql('SELECT 1;', user='root', password='', database=SCRATCH_DB)
                local.require(probe.returncode == 0, 'SCRATCH_DB_CONNECT_FAILED')
                print('preflight: MySQL3306=available Redis6379=available scratch_db=' + SCRATCH_DB, flush=True)

                # 1) 类型检查 / 构建原始输出。
                stage(log, ['npm', '--prefix', 'web', 'run', 'typecheck'], ROOT, clean, 300)
                stage(log, ['npm', '--prefix', 'web', 'run', 'build'], ROOT, clean, 300)

                with tempfile.TemporaryDirectory(prefix='.table-template-', dir=BROWSER) as directory:
                    temp = Path(directory)
                    binary = temp / 'server'
                    result = stage(log, ['go', 'build', '-mod=readonly', '-o', str(binary), './cmd/server'],
                                   ROOT, clean)
                    local.require(result.returncode == 0, 'GO_BUILD_FAILED')

                    config = temp / 'config.yaml'
                    config.write_text(CONFIG_TEMPLATE.format(db=SCRATCH_DB), encoding='utf-8')

                    service_env = local.service_env(action='dev', explicit={
                        'POKER_HTTP_HOST': '127.0.0.1', 'POKER_HTTP_PORT': '8081',
                        'POKER_AI_TURN_DELAY': '200ms', 'POKER_HUMAN_ACTION_TIMEOUT': '5s',
                    })
                    service_env['POKER_CONFIG_FILE'] = str(config)
                    # Redis 密码仅存在于子进程环境，wrapper 自身不打印。
                    service_env['POKER_REDIS_PASSWORD'] = storage['redis_password']

                    # 2) 迁移 scratch 库到最新 schema。
                    migrated = stage(log, [str(binary), '-conf', str(config), 'migrate'], ROOT, service_env, 120)
                    local.require(migrated.returncode == 0, 'SCRATCH_MIGRATE_FAILED')
                    rc, version = scratch_schema_version()
                    local.require(rc == 0 and version == '2', 'SCRATCH_SCHEMA_VERSION_NOT_2')

                    local.stop_stale_server(8081)
                    server = subprocess.Popen([str(binary), '-conf', str(config)], cwd=ROOT, env=service_env,
                                              start_new_session=True,
                                              stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
                    deadline = time.monotonic() + 40
                    healthy = None
                    while time.monotonic() < deadline:
                        local.require(server.poll() is None, 'GO_START_FAILED_LOGS_NOT_EXPOSED')
                        try:
                            status, body = get('/api/v1/health')
                            if status == 200:
                                healthy = json.loads(body)
                                break
                        except (OSError, urllib.error.URLError, ValueError):
                            pass
                        time.sleep(0.1)
                    local.require(healthy is not None and healthy['mysql'] == healthy['redis'] == 'available',
                                  'HEALTH_FAILED')
                    line = 'health: ' + json.dumps(healthy) + '; scratch_schema_version=' + version
                    print(line, flush=True)
                    log.write('\n```text\n' + line + '\n```\n')

                    screen_dir = BROWSER / 'table-template-artifacts' / time.strftime('%Y%m%d-%H%M%S')
                    screen_dir.mkdir(parents=True, exist_ok=True)
                    browser_env = {**clean, 'POKER_BASE_URL': BASE, 'SCREEN_DIR': str(screen_dir),
                                   'FORCE_COLOR': '0'}
                    result = stage(log, ['npx', '--no-install', 'playwright', 'test',
                                         '--config=playwright.tabletemplate.config.ts'],
                                   BROWSER, browser_env, 900)
                    counts = []
                    for label in ('passed', 'failed', 'did not run'):
                        match = re.search(r'(\d+) ' + label, result.stdout)
                        counts.append(int(match.group(1)) if match else 0)
                    line = f'playwright: passed={counts[0]} failed={counts[1]} not_run={counts[2]} exit={result.returncode}'
                    print(line, flush=True)
                    log.write('\n```text\n' + line + '\n```\n\n截图目录：`' + str(screen_dir) + '`。\n')
                    local.require(result.returncode == 0, 'REAL_BROWSER_FAILED')
                    local.require(counts == [8, 0, 0], 'EXPECTED_8_TABLE_TEMPLATE_TESTS')
            except Exception as error:
                code = str(error) if isinstance(error, local.SafeError) else 'HARNESS_FAILED_REDACTED'
                failures.append(code)
                print('table-template-e2e: FAIL ' + code, flush=True)
                log.write('\n联调停止：`' + code + '`。\n')
            finally:
                stop(server)
                dropped = drop_scratch_db()
                released = not local.port_open(8081)
                retained = local.port_open(3306) and local.port_open(6379)
                if dropped.returncode != 0:
                    failures.append('SCRATCH_DB_DROP_FAILED')
                line = ('cleanup: own_server_stopped=' + str(server is None or server.poll() is not None) +
                        '; scratch_db_dropped=' + str(dropped.returncode == 0) +
                        '; 8081_released=' + str(released) +
                        '; Homebrew_3306_6379_retained=' + str(retained))
                print(line, flush=True)
                log.write('\n```text\n' + line + '\n```\n')
                if not released or not retained:
                    failures.append('CLEANUP_OR_DEPENDENCY_CHECK_FAILED')
                log.write('\n## 结论\n\n' +
                          ('FAIL / 部分完成，见完整失败输出。' if failures else
                           'PASS：8 个真实浏览器场景 + typecheck/build/Go 构建全部通过。') + '\n')
    except Exception:
        print('TABLE_TEMPLATE_WRAPPER_FAILED_REDACTED', file=sys.stderr)
        return 1
    return 1 if failures else 0


if __name__ == '__main__':
    raise SystemExit(main())
