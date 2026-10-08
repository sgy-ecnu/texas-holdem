#!/usr/bin/env python3
"""「按座位选模型」真实链路验收 wrapper。

职责边界（与 run_human.py / run_turn.py 同源安全约定）：
- 只启动/停止本轮自己拉起的真实 Go 子进程与受控本地 provider server；凭据由 local.py/Go
  在进程内从受保护配置载入，绝不读取、打印或回传任何凭据。
- 受控 provider 不是真实模型：仅统计出站请求数并返回 OpenAI 兼容最小响应。
- 依赖（MySQL 3306 / Redis 6379）保留不停止；结束必释放 8081 与 provider 端口。
- 不 commit/push。完整命令原始输出写入 docs/SEAT_MODEL_SELECT.md。
用法：python3 tests/e2e/browser/run_seat_model.py
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
import time
import urllib.error
import urllib.request

ROOT = Path(__file__).resolve().parents[3]
BROWSER = Path(__file__).resolve().parent
REPORT = ROOT / 'docs/SEAT_MODEL_SELECT.md'
BASE = 'http://127.0.0.1:8081'
PROVIDER_PORT = 4390
PROVIDER_URL = f'http://127.0.0.1:{PROVIDER_PORT}'

spec = importlib.util.spec_from_file_location('local', ROOT / 'scripts/local.py')
assert spec and spec.loader
local = importlib.util.module_from_spec(spec)
spec.loader.exec_module(local)
HTTP = urllib.request.build_opener(urllib.request.ProxyHandler({}))


def stage(log, argv, cwd, env, timeout=900):
    command = shlex.join(argv)
    print('$ ' + command, flush=True)
    result = subprocess.run(argv, cwd=cwd, env=env, capture_output=True, text=True, timeout=timeout)
    output = local.sanitize_output(result.stdout + result.stderr)
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
        return response.status, json.load(response)


def provider_count():
    with HTTP.open(PROVIDER_URL + '/__count', timeout=3) as response:
        return json.load(response)['count']


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


def main():
    failures = []
    server = None
    provider = None
    clean = local.child_env()
    REPORT.write_text('# 按座位选模型：修复「选了真实模型不生效」验证证据\n\n'
                      '运行时间：' + time.strftime('%Y-%m-%d %H:%M:%S %z') + '\n\n'
                      '统一判定规则：**真实调用 = mode==live 且当前 AI 座位 model_ref != "mock"**。\n'
                      '复现命令（项目根目录）：`python3 tests/e2e/browser/run_seat_model.py`。\n'
                      '真实 Go 二进制 + 嵌入最新 web/dist + Chromium + 受控本地 provider（非真实模型）。\n'
                      '不读取 .env.local / config.local.yaml 的值；连接凭据仅由 local.py/Go 本地载入，不回传。\n', encoding='utf-8')
    with REPORT.open('a', encoding='utf-8') as log:
        try:
            local.require(not local.port_open(PROVIDER_PORT), 'PROVIDER_PORT_IN_USE_STOP_NO_KILL')
            storage = local.storage_config()
            local.require(storage['mysql_host'] == '127.0.0.1' and storage['mysql_port'] == 3306 and
                          storage['redis_port'] == 6379, 'HOMEBREW_ENDPOINT_REQUIRED')
            probe = local.mysql_query(storage, 'SELECT 1; SELECT COUNT(*) FROM games WHERE active_slot=1;')
            local.require(probe.returncode == 0, 'MYSQL_PREFLIGHT_FAILED_REDACTED')
            local.require(probe.stdout.split() == ['1', '0'], 'EXISTING_ACTIVE_GAME_STOP')
            local.redis_ping(storage)
            print('preflight: MySQL3306=available Redis6379=available active_games=0', flush=True)

            # 1) 单测 / 类型 / 构建 / vet 原始输出。
            stage(log, ['npm', '--prefix', 'web', 'run', 'typecheck'], ROOT, clean, 300)
            stage(log, ['npm', '--prefix', 'web', 'run', 'build'], ROOT, clean, 300)
            stage(log, ['go', 'test', '-mod=readonly', '-race', '-count=1', './internal/service/'], ROOT, clean, 900)
            stage(log, ['go', 'vet', './internal/service/'], ROOT, clean, 300)

            with tempfile.TemporaryDirectory(prefix='.seat-model-', dir=BROWSER) as directory:
                temp = Path(directory)
                catalog = temp / 'catalog.json'
                catalog.write_text(json.dumps({
                    'provider': {
                        'local': {
                            'npm': '@ai-sdk/openai-compatible',
                            'options': {
                                'baseURL': PROVIDER_URL,
                                'apiKey': 'local-test-placeholder',
                            },
                            'models': {'real': {'id': 'local-real'}},
                        }
                    }
                }), encoding='utf-8')

                provider = subprocess.Popen(
                    ['node', str(BROWSER / 'seat-model' / 'provider-server.mjs')],
                    cwd=BROWSER, env={**clean, 'PROVIDER_PORT': str(PROVIDER_PORT)},
                    stdout=subprocess.DEVNULL, stderr=subprocess.STDOUT, start_new_session=True)
                deadline = time.monotonic() + 20
                while time.monotonic() < deadline and port_free(PROVIDER_PORT):
                    local.require(provider.poll() is None, 'PROVIDER_START_FAILED')
                    time.sleep(0.1)
                local.require(not port_free(PROVIDER_PORT), 'PROVIDER_START_TIMEOUT')

                binary = temp / 'server'
                result = stage(log, ['go', 'build', '-mod=readonly', '-o', str(binary), './cmd/server'],
                               ROOT, clean)
                local.require(result.returncode == 0, 'GO_BUILD_FAILED')

                local.stop_stale_server(8081)
                env = local.service_env(action='dev-live', explicit={
                    'POKER_HTTP_HOST': '127.0.0.1', 'POKER_HTTP_PORT': '8081',
                    'POKER_CATALOG_PATH': str(catalog),
                    'POKER_AI_TURN_DELAY': '150ms', 'POKER_HUMAN_ACTION_TIMEOUT': '5s',
                })
                server = subprocess.Popen([str(binary)], cwd=ROOT, env=env, start_new_session=True,
                                          stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
                deadline = time.monotonic() + 40
                healthy = None
                while time.monotonic() < deadline:
                    local.require(server.poll() is None, 'GO_START_FAILED_LOGS_NOT_EXPOSED')
                    try:
                        status, health = get('/api/v1/health')
                        if status == 200:
                            healthy = health
                            break
                    except (OSError, urllib.error.URLError):
                        pass
                    time.sleep(0.1)
                local.require(healthy is not None and healthy['mysql'] == healthy['redis'] == 'available'
                              and healthy['allow_paid_models'] is True, 'HEALTH_OR_PAID_FLAG_FAILED')
                status, models = get('/api/v1/models')
                refs = [m['ref'] for m in models['models']]
                local.require(status == 200 and 'mock' in refs and 'local/real' in refs, 'CATALOG_MISSING_REAL')
                line = 'health: ' + json.dumps(healthy) + '; models=' + json.dumps(refs)
                print(line, flush=True)
                log.write('\n```text\n' + line + '\n```\n')

                screen_dir = BROWSER / 'seat-model-artifacts' / time.strftime('%Y%m%d-%H%M%S')
                screen_dir.mkdir(parents=True, exist_ok=True)
                browser_env = {**clean, 'POKER_BASE_URL': BASE, 'PROVIDER_URL': PROVIDER_URL,
                               'SCREEN_DIR': str(screen_dir), 'FORCE_COLOR': '0'}
                result = stage(log, ['npx', '--no-install', 'playwright', 'test',
                                     '--config=playwright.seatmodel.config.ts'],
                               BROWSER, browser_env, 600)
                counts = []
                for label in ('passed', 'failed', 'did not run'):
                    match = re.search(r'(\d+) ' + label, result.stdout)
                    counts.append(int(match.group(1)) if match else 0)
                line = f'playwright: passed={counts[0]} failed={counts[1]} not_run={counts[2]} exit={result.returncode}'
                print(line, flush=True)
                log.write('\n```text\n' + line + '\n```\n\n截图目录：`' + str(screen_dir) + '`。\n')
                local.require(result.returncode == 0, 'REAL_BROWSER_FAILED')
                local.require(counts == [4, 0, 0], 'EXPECTED_4_SEAT_MODEL_TESTS')
        except Exception as error:
            code = str(error) if isinstance(error, local.SafeError) else 'HARNESS_FAILED_REDACTED'
            failures.append(code)
            print('seat-model-e2e: FAIL ' + code, flush=True)
            log.write('\n联调停止：`' + code + '`。\n')
        finally:
            stop(server)
            stop(provider)
            released = not local.port_open(8081) and port_free(PROVIDER_PORT)
            retained = local.port_open(3306) and local.port_open(6379)
            line = ('cleanup: own_server_stopped=' + str(server is None or server.poll() is not None) +
                    '; provider_stopped=' + str(provider is None or provider.poll() is not None) +
                    '; 8081_and_provider_released=' + str(released) +
                    '; Homebrew_3306_6379_retained=' + str(retained))
            print(line, flush=True)
            log.write('\n```text\n' + line + '\n```\n')
            if not released or not retained:
                failures.append('CLEANUP_OR_DEPENDENCY_CHECK_FAILED')
            log.write('\n## 结论\n\n' +
                      ('FAIL / 部分完成，见完整失败输出。' if failures else
                       'PASS：4 个真实浏览器场景 + 单测/类型/构建/vet 全部通过。') + '\n')
    return 1 if failures else 0


if __name__ == '__main__':
    raise SystemExit(main())
