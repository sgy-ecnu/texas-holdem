#!/usr/bin/env python3
"""谁赢了结算 / 街进度指示器 / 模型短名 真实链路验收 wrapper。

职责边界（与 run_action_popover.py / run_browser.py 同源的安全约定）：
- 只负责重新构建 web/dist 与 Go 二进制、启动/停止本轮自己拉起的真实 Go 子进程；
  等价于 `make run`（先编译前端生成最新 dist，再前台启动嵌入最新 dist 的 Go 服务），
  但为了自动化与可回收，直接在临时目录运行本轮新构建二进制。
- 启动前用 scripts/local.py 的 stop_stale_server 清理 8081 上本项目残留 server
  （陌生进程拒绝并中止，绝不误杀）。
- 绝不读取、打印或转发任何凭据；服务原始日志丢弃，不回传。
- 依赖（MySQL 3306 / Redis 6379）保留不停止；结束必释放 8081。
- mock 目录、allow_paid_models=false；不产生真实 provider 请求；不 commit/push。
用法：
  python3 tests/e2e/browser/run_settlement.py
"""
import importlib.util
import json
import os
from pathlib import Path
import re
import shlex
import signal
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request

ROOT = Path(__file__).resolve().parents[3]
BROWSER = Path(__file__).resolve().parent
REPORT = ROOT / 'docs/SETTLEMENT_E2E.md'
spec = importlib.util.spec_from_file_location('local', ROOT / 'scripts/local.py')
assert spec and spec.loader
local = importlib.util.module_from_spec(spec)
spec.loader.exec_module(local)
HTTP = urllib.request.build_opener(urllib.request.ProxyHandler({}))
BASE = 'http://127.0.0.1:8081'


def get(path):
    try:
        response = HTTP.open(BASE + path, timeout=3)
    except urllib.error.HTTPError as error:
        response = error
    with response:
        return response.status, json.load(response)


def stage(log, argv, cwd, env, timeout=600):
    command = shlex.join(argv)
    print('$ ' + command, flush=True)
    result = subprocess.run(argv, cwd=cwd, env=env, capture_output=True, text=True, timeout=timeout)
    # 凭据只在连接进程内；保留完整测试输出，仅作凭据脱敏。
    output = local.sanitize_output(result.stdout + result.stderr)
    output = re.sub(r'(?i)(poker_player=)[^;\s]+', r'\1[凭据已脱敏]', output)
    log.write('\n### 命令\n\n```sh\n' + command + '\n```\n\n工作目录：`' + str(cwd) +
              '`\n\n```text\n' + output + ('' if output.endswith('\n') else '\n') +
              '```\n\n退出码：' + str(result.returncode) + '\n')
    log.flush()
    print(output, end='' if output.endswith('\n') else '\n', flush=True)
    print('exit=' + str(result.returncode), flush=True)
    return result


def stop(process):
    if process and process.poll() is None:
        os.killpg(process.pid, signal.SIGTERM)
        try:
            process.wait(timeout=20)
        except subprocess.TimeoutExpired:
            os.killpg(process.pid, signal.SIGKILL)
            process.wait()
            raise local.SafeError('OWN_SERVER_STOP_TIMEOUT')


def main():
    process = None
    browser_result = None
    failure = None
    clean = local.child_env()
    stamp = time.strftime('%Y%m%d-%H%M%S')
    screen_dir = BROWSER / 'settlement' / 'artifacts' / stamp
    screen_dir.mkdir(parents=True, exist_ok=True)
    # 追加每轮原始证据，重跑不得覆盖失败上下文。
    with REPORT.open('a', encoding='utf-8') as log:
        log.write('\n# 谁赢了结算 / 街进度 / 模型短名 真实链路验收证据\n\n'
                  '入口：`python3 tests/e2e/browser/run_settlement.py`。时间：' +
                  time.strftime('%Y-%m-%d %H:%M:%S %z') + '\n\n'
                  '真实 Chromium → 同源 Go 嵌入 Web → Homebrew MySQL 3306 / Redis 6379。'
                  '无 fixture/route mock；mock 模型目录、allow_paid_models=false。\n\n'
                  '启动方式等价于 `make run`：先 `npm --prefix web run build` 生成最新 web/dist，'
                  '再 `go build -mod=readonly -o <tmp>/server ./cmd/server` 产出含 showdown 字段与'
                  '手间窗口的最新二进制，随后前台运行该二进制。启动前经 `scripts/local.py:stop_stale_server`'
                  '清理 8081 残留 server。显式非敏感覆盖：POKER_HTTP_HOST=127.0.0.1、'
                  'POKER_HTTP_PORT=8081、POKER_HUMAN_ACTION_TIMEOUT=5s、POKER_CATALOG_PATH=scripts/mock-catalog.json、'
                  'POKER_ALLOW_PAID_MODELS=false；AI 步进沿用 1.5s、手间窗口默认摊牌 12s/弃牌 8s。'
                  '不读取 .env.local / config.local.yaml 的值，不提取 Cookie 凭据；'
                  '存储连接只由 local.py 连接助手/Go 进程在本地加载。禁用 trace/HAR/video。\n\n'
                  '截图证据目录：`' + str(screen_dir.relative_to(ROOT)) + '`。\n\n')
        try:
            # 清理本项目残留 server；陌生进程会抛 SafeError 中止，绝不误杀。
            try:
                local.stop_stale_server()
            except local.SafeError:
                raise
            local.require(not local.port_open(8081), 'PORT_8081_IN_USE_STOP_NO_KILL')
            # 只在本地连接助手内消费受保护存储配置；不回传任何值或原始失败输出。
            storage = local.storage_config()
            local.require(storage['mysql_host'] == '127.0.0.1' and storage['mysql_port'] == 3306 and
                          storage['redis_port'] == 6379, 'HOMEBREW_ENDPOINT_REQUIRED')
            probe = local.mysql_query(storage, 'SELECT 1; SELECT COUNT(*) FROM games WHERE active_slot=1;')
            local.require(probe.returncode == 0, 'MYSQL_PREFLIGHT_FAILED_REDACTED')
            local.require(probe.stdout.split() == ['1', '0'], 'EXISTING_ACTIVE_GAME_STOP')
            local.redis_ping(storage)
            print('preflight: mysql=3306 available; redis=6379 available; active_games=0; 8081=free', flush=True)
            log.write('\n预检：MySQL 3306 / Redis 6379 连接成功；active_games=0；8081 空闲。\n')
            build = stage(log, ['npm', '--prefix', 'web', 'run', 'build'], ROOT, clean)
            local.require(build.returncode == 0, 'FRONTEND_BUILD_FAILED')
            with tempfile.TemporaryDirectory(prefix='.settle-runtime-', dir=BROWSER) as directory:
                temp = Path(directory)
                binary = temp / 'server'
                result = stage(log, ['go', 'build', '-mod=readonly', '-o', str(binary), './cmd/server'], ROOT, clean)
                local.require(result.returncode == 0, 'GO_BUILD_FAILED')
                env = local.service_env(e2e=True, explicit={
                    'POKER_HTTP_HOST': '127.0.0.1', 'POKER_HTTP_PORT': '8081',
                    'POKER_HUMAN_ACTION_TIMEOUT': '5s',
                })
                config = local.config_public(env=env)
                local.require(config['http_base_url'] == BASE, 'CONFIG_BASE_URL_MISMATCH')
                log.write('\n### 启动\n\n'
                          '等价于 local.py dev 的 `service_env(e2e=True, explicit=...)` 环境，直接运行本轮新构建二进制。'
                          '服务原始日志不保存、不回传，避免凭据进入证据。\n')
                process = subprocess.Popen([str(binary)], cwd=ROOT, env=env,
                                           stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
                                           start_new_session=True)
                deadline = time.monotonic() + 40
                state = None
                while time.monotonic() < deadline:
                    local.require(process.poll() is None, 'GO_SERVER_START_FAILED_LOGS_NOT_EXPOSED')
                    try:
                        status, state = get('/api/v1/health')
                        if status == 200:
                            break
                    except (OSError, urllib.error.URLError):
                        pass
                    time.sleep(0.1)
                local.require(state and state['mysql'] == state['redis'] == 'available' and
                              state['allow_paid_models'] is False, 'HEALTH_OR_PAID_GUARD_FAILED')
                status, models = get('/api/v1/models')
                local.require(status == 200 and [m['ref'] for m in models['models']] == ['mock'],
                              'MOCK_ONLY_CATALOG_REQUIRED')
                print('health: ' + json.dumps(state) + '; models=[mock]', flush=True)
                log.write('\n实测 health：`' + json.dumps(state) + '`；models 仅 mock。\n')
                browser_env = {**clean, 'POKER_BASE_URL': BASE,
                               'SETTLE_SCREEN_DIR': str(screen_dir),
                               'SETTLE_EVIDENCE_FILE': str(temp / 'evidence.json'),
                               'SETTLE_OUTPUT_DIR': str(temp / 'results'),
                               'FORCE_COLOR': '0'}
                browser_result = stage(log, ['npx', '--no-install', 'playwright', 'test',
                                             '--config=playwright.settlement.config.ts'], BROWSER, browser_env, 600)
                counts = []
                for label in ('passed', 'failed', 'did not run'):
                    match = re.search(r'(\d+) ' + label, browser_result.stdout)
                    counts.append(int(match.group(1)) if match else 0)
                summary = f'playwright: passed={counts[0]} failed={counts[1]} not_run={counts[2]} exit={browser_result.returncode}'
                print(summary, flush=True)
                log.write('\n```text\n' + summary + '\n```\n')
                local.require(browser_result.returncode == 0, 'REAL_BROWSER_FAILED_STOP_IMPLEMENTATION_UNCHANGED')
        except Exception as error:
            failure = str(error) if isinstance(error, local.SafeError) else 'HARNESS_FAILED_REDACTED'
            print('settlement-e2e: FAIL ' + failure, flush=True)
            log.write('\n联调停止：`' + failure + '`。未修改 Go/Web 实现。\n')
        finally:
            stop(process)
            released = not local.port_open(8081)
            retained = local.port_open(3306) and local.port_open(6379)
            line = 'cleanup: own_server_stopped=' + str(process is None or process.poll() is not None) + \
                   '; 8081_released=' + str(released) + '; Homebrew_3306_6379_retained=' + str(retained)
            print(line, flush=True)
            log.write('\n```text\n' + line + '\n```\n')
            if not released or not retained:
                failure = failure or 'CLEANUP_OR_DEPENDENCY_CHECK_FAILED'
        race = stage(log, ['go', 'test', '-mod=readonly', '-race', '-count=1', './...'], ROOT, clean, 1200)
        passed_packages = len(re.findall(r'^ok\s', race.stdout, re.M))
        failed_packages = len(re.findall(r'^FAIL\s', race.stdout, re.M))
        line = f'go-race: passed_packages={passed_packages} failed_packages={failed_packages} exit={race.returncode}'
        print(line, flush=True)
        log.write('\n```text\n' + line + '\n```\n')
        if race.returncode:
            failure = failure or 'GO_RACE_FAILED'
        log.write('\n## 场景与测试函数\n\n'
                  '| 场景 | 测试函数 | 证据 |\n|---|---|---|\n'
                  '| 1a 模型短名上移第一行 / 第二行剩余不换行 | settlement_showdown | seat_facts EVIDENCE + street-preflop.png |\n'
                  '| 1b 街轨道 5 节点 + 状态行 + 切街 1.5s 提示 | settlement_showdown | street-*.png + transition_text EVIDENCE |\n'
                  '| 1c 摊牌结算（翻牌/亮金/赢+X/筹码飞/结算条） | settlement_showdown | settlement-*.png + EVIDENCE |\n'
                  '| 1d API CompletedHandDTO.showdown 字段 | settlement_showdown | api_showdown EVIDENCE |\n'
                  '| 2 弃牌结束：弃牌者不翻 + hole_cards 保密 | settlement_uncontested | settlement-uncontested.png + EVIDENCE |\n\n'
                  '测试源码：`tests/e2e/browser/settlement/settlement.spec.ts`。'
                  '结果以未截断原始输出为准。\n\n'
                  '未验证边界：mock 模型短名未收录（`web/src/format.ts:MODEL_SHORT_LABEL` 只映射 '
                  '`deepseek/deepseek-v4-pro`），故「AI-x (V4 Pro)」短名文本无法在真实链路出现，'
                  '只记录「mock 省略短名 + title 保留 label」的实际现象；付费/真实模型、多真人、'
                  '边池/退款非零、非 loopback 部署、故障注入未覆盖。\n\n'
                  '最终状态：' + ('FAIL / 部分完成，见上文失败证据。' if failure else 'PASS / 本轮真实链路与 Go race 均通过。') + '\n')
    return 1 if failure else 0


if __name__ == '__main__':
    raise SystemExit(main())
