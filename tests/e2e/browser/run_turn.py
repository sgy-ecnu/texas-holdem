#!/usr/bin/env python3
"""轮转真实联调入口；复用安全启动助手，不修改旧真人报告或 fixture。"""
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile
import time
import urllib.error

import run_human as harness

ROOT, BROWSER, local = harness.ROOT, harness.BROWSER, harness.local
REPORT = ROOT / 'docs/TURN_E2E.md'


def main():
    process = None
    failures = []
    clean = local.child_env()
    with REPORT.open('a', encoding='utf-8') as log:
        log.write('\n# 轮转 UI 真实端到端证据\n\n运行时间：' + time.strftime('%Y-%m-%d %H:%M:%S %z') + '\n\n'
                  '复现命令（项目根目录）：`python3 tests/e2e/browser/run_turn.py`。\n'
                  '真实 Go 新构建二进制/嵌入 Web + Chromium + Homebrew MySQL3306/Redis6379。'
                  'mock-only、allow_paid_models=false、真人超时 5s、AI 步进 800ms（显式短值，仅 mock 生效）。\n'
                  '不读取 .env.local；连接凭据仅由 local.py 连接助手/Go 在本地加载，受保护配置内容不回传。'
                  '不读取用户模型目录，不打印 Cookie 凭据；Set-Cookie 回归仅检查属性布尔值。'
                  '不启用 trace/HAR/video/storageState；截图只有渲染 UI，不含请求头。\n'
                  '本地无 Git 元数据，未 pull、commit、push，版本新旧未核验。\n')
        try:
            local.require(not local.port_open(8081), 'PORT_8081_IN_USE_STOP_NO_KILL')
            storage = local.storage_config()
            local.require(storage['mysql_host'] == '127.0.0.1' and storage['mysql_port'] == 3306 and
                          storage['redis_port'] == 6379, 'HOMEBREW_ENDPOINT_REQUIRED')
            probe = local.mysql_query(storage, 'SELECT 1; SELECT COUNT(*) FROM games WHERE active_slot=1;')
            local.require(probe.returncode == 0, 'MYSQL_PREFLIGHT_FAILED_REDACTED')
            local.require(probe.stdout.split() == ['1', '0'], 'EXISTING_ACTIVE_GAME_STOP')
            local.redis_ping(storage)
            preflight = 'preflight: MySQL3306=available Redis6379=available active_games=0 8081=free'
            print(preflight, flush=True)
            log.write('\n```text\n' + preflight + '\n```\n')
            result = harness.stage(log, ['npm', '--prefix', 'web', 'run', 'build'], ROOT, clean)
            local.require(result.returncode == 0, 'WEB_BUILD_FAILED')
            with tempfile.TemporaryDirectory(prefix='.turn-runtime-', dir=BROWSER) as directory:
                temp = Path(directory)
                binary = temp / 'server'
                result = harness.stage(log, ['go', 'build', '-mod=readonly', '-o', str(binary), './cmd/server'],
                                       ROOT, clean)
                local.require(result.returncode == 0, 'GO_BUILD_FAILED')
                env = local.service_env(e2e=True, explicit={
                    'POKER_HTTP_HOST': '127.0.0.1', 'POKER_HTTP_PORT': '8081',
                    'POKER_HUMAN_ACTION_TIMEOUT': '5s', 'POKER_AI_TURN_DELAY': '800ms',
                })
                local.require(local.config_public(env=env)['http_base_url'] == harness.BASE,
                              'CONFIG_BASE_URL_MISMATCH')
                log.write('\n## 启动方式\n\n使用 `local.service_env(e2e=True, explicit=...)` 的等价 dev 环境。'
                          '显式覆盖 POKER_HTTP_HOST=127.0.0.1、POKER_HTTP_PORT=8081、'
                          'POKER_HUMAN_ACTION_TIMEOUT=5s、POKER_AI_TURN_DELAY=800ms；'
                          'E2E 强制 POKER_ALLOW_PAID_MODELS=false 与 scripts/mock-catalog.json。'
                          '直接运行本轮二进制：`' + str(binary) + '`，cwd 为项目根目录。'
                          '服务原始日志丢弃，避免凭据出现在证据中。\n')
                process = subprocess.Popen([str(binary)], cwd=ROOT, env=env, start_new_session=True,
                                           stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
                deadline = time.monotonic() + 40
                healthy = False
                while time.monotonic() < deadline:
                    local.require(process.poll() is None, 'GO_START_FAILED_LOGS_NOT_EXPOSED')
                    try:
                        status, health = harness.get('/api/v1/health')
                        if status == 200:
                            healthy = True
                            break
                    except (OSError, urllib.error.URLError):
                        pass
                    time.sleep(0.1)
                local.require(healthy and health['mysql'] == health['redis'] == 'available' and
                              health['allow_paid_models'] is False, 'HEALTH_OR_PAID_GUARD_FAILED')
                status, models = harness.get('/api/v1/models')
                local.require(status == 200 and [model['ref'] for model in models['models']] == ['mock'],
                              'MOCK_ONLY_REQUIRED')
                line = 'health: ' + json.dumps(health) + '; models=[mock]'
                print(line, flush=True)
                log.write('\n```text\n' + line + '\n```\n')
                screen_dir = BROWSER / 'turn-artifacts' / time.strftime('%Y%m%d-%H%M%S')
                screen_dir.mkdir(parents=True, exist_ok=True)
                browser_env = {**clean, 'POKER_BASE_URL': harness.BASE, 'HUMAN_OUTPUT_DIR': str(temp / 'artifacts'),
                               'HUMAN_EVIDENCE_FILE': str(temp / 'human-evidence.json'),
                               'TURN_SCREEN_DIR': str(screen_dir), 'FORCE_COLOR': '0'}
                result = harness.stage(log, ['npx', '--no-install', 'playwright', 'test',
                                             '--config=playwright.turn.config.ts'], BROWSER, browser_env, 360)
                counts = []
                for label in ('passed', 'failed', 'did not run'):
                    match = re.search(r'(\d+) ' + label, result.stdout)
                    counts.append(int(match.group(1)) if match else 0)
                line = f'playwright: passed={counts[0]} failed={counts[1]} not_run={counts[2]} exit={result.returncode}'
                print(line, flush=True)
                log.write('\n```text\n' + line + '\n```\n\n截图目录：`' + str(screen_dir) + '`。\n')
                local.require(result.returncode == 0, 'REAL_BROWSER_FAILED_STOP_IMPLEMENTATION_UNCHANGED')
                local.require(counts == [7, 0, 0], 'EXPECTED_3_TURN_AND_4_HUMAN_TESTS')
        except Exception as error:
            code = str(error) if isinstance(error, local.SafeError) else 'HARNESS_FAILED_REDACTED'
            failures.append(code)
            print('turn-e2e: FAIL ' + code, flush=True)
            log.write('\n联调停止：`' + code + '`；不修改 Go/Web 实现。\n')
        finally:
            harness.stop(process)
            released = not local.port_open(8081)
            retained = local.port_open(3306) and local.port_open(6379)
            line = 'cleanup: own_server_stopped=' + str(process is None or process.poll() is not None) + \
                   '; 8081_released=' + str(released) + '; Homebrew_3306_6379_retained=' + str(retained)
            print(line, flush=True)
            log.write('\n```text\n' + line + '\n```\n')
            if not released or not retained:
                failures.append('CLEANUP_OR_DEPENDENCY_CHECK_FAILED')
        result = harness.stage(log, ['go', 'test', '-mod=readonly', '-race', '-count=1', './...'], ROOT, clean, 1200)
        passed = len(re.findall(r'^ok\s', result.stdout, re.M))
        failed = len(re.findall(r'^FAIL\s', result.stdout, re.M))
        line = f'go-race: passed_packages={passed} failed_packages={failed} exit={result.returncode}'
        print(line, flush=True)
        log.write('\n```text\n' + line + '\n```\n')
        if result.returncode:
            failures.append('GO_RACE_FAILED')
        log.write('\n## 场景 → 测试函数 → 结果证据\n\n'
                  '| 场景 | 测试函数 | 结果依据 |\n|---|---|---|\n'
                  '| AI 光圈逐座轮转、思考提示、行动间隔 | turn_mock_cadence_feed | TURN_EVIDENCE mock-cadence |\n'
                  '| 历史逐条增加与自动滚到底部 | turn_mock_cadence_feed | ui_feed_additions + scroll + 截图 |\n'
                  '| 真人光圈、底部倒计时减少、合法按钮 | turn_human_countdown | TURN_EVIDENCE human-countdown |\n'
                  '| 停顿窗口 pause/stop 快速打断与稳定 | turn_interrupt | TURN_EVIDENCE interrupt |\n'
                  '| 既有真人真实回归 | human.spec.ts 的全部4个测试 | EVIDENCE + Playwright逐条结果 |\n\n'
                  '没有对应通过输出的场景不得认定通过。完整测试断言见 '
                  '`tests/e2e/browser/turn/turn.spec.ts` 和 `tests/e2e/browser/human/human.spec.ts`。\n\n'
                  '未验证边界：**live 真实模型耗时节奏未联网验证**；未调用付费模型；'
                  '仅桌面 Chromium，未验证移动端/其他浏览器、极慢网络/后台标签限流、'
                  '动作多于60条时的历史截断、全押实际提交、跨标签竞争、服务重启和存储故障。'
                  'AI 行动时间为浏览器观测时间，不是服务端数据库提交时钟。'
                  '原 fixture 与旧观战场景未重跑，未改变其机制。\n\n'
                  '本轮结论：' + ('FAIL / 部分完成，见完整失败输出。' if failures else
                                  'PASS / 3个轮转测试、4个真人回归、Go race均通过。') + '\n')
    return 1 if failures else 0


if __name__ == '__main__':
    raise SystemExit(main())
