#!/usr/bin/env python3
"""脚本安全测试；只使用临时文件和假值，不读取真实 .env.local。"""
import contextlib
import importlib.util
import io
import json
import os
from pathlib import Path
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location("local", ROOT / "scripts/local.py")
assert spec is not None and spec.loader is not None
local = importlib.util.module_from_spec(spec)
spec.loader.exec_module(local)


class LocalSafety(unittest.TestCase):
    def values(self):
        return {"POKER_MYSQL_DSN": "texas_holdem_app:" + "x" * 48 +
                "@tcp(127.0.0.1:3306)/texas_holdem?parseTime=true",
                "POKER_REDIS_ADDR": "127.0.0.1:6379", "POKER_REDIS_DB": "15",
                "POKER_CATALOG_PATH": str(local.CATALOG), "POKER_ALLOW_PAID_MODELS": "false"}

    def test_json_env_roundtrip(self):
        values = self.values()
        parsed = local.parse_env("\n".join(k + "=" + json.dumps(v) for k, v in values.items()))
        self.assertTrue(parsed == values)
        local.validate(parsed)

    def test_no_shell_expansion(self):
        parsed = local.parse_env('POKER_REDIS_PASSWORD="$(touch forbidden)"')
        self.assertTrue(parsed["POKER_REDIS_PASSWORD"].startswith("$("))

    def test_duplicate_rejected(self):
        with self.assertRaises(local.SafeError):
            local.parse_env('POKER_REDIS_DB="15"\nPOKER_REDIS_DB="15"')

    def test_unknown_key_rejected(self):
        with self.assertRaises(local.SafeError):
            local.parse_env('UNTRUSTED="value"')

    def test_nonlocal_mysql_rejected(self):
        values = self.values()
        values["POKER_MYSQL_DSN"] = values["POKER_MYSQL_DSN"].replace("127.0.0.1", "192.0.2.1")
        with self.assertRaises(local.SafeError):
            local.validate(values)

    def test_nonlocal_redis_rejected(self):
        values = self.values()
        values["POKER_REDIS_ADDR"] = "192.0.2.1:6379"
        with self.assertRaises(local.SafeError):
            local.validate(values)

    def test_real_catalog_rejected(self):
        values = self.values()
        values["POKER_CATALOG_PATH"] = "/not-a-mock-catalog"
        with self.assertRaises(local.SafeError):
            local.validate(values)

    def test_paid_models_rejected(self):
        values = self.values()
        values["POKER_ALLOW_PAID_MODELS"] = "true"
        with self.assertRaises(local.SafeError):
            local.validate(values)

    def test_permissions_and_symlink(self):
        (ROOT / "tmp").mkdir(exist_ok=True)
        with tempfile.TemporaryDirectory(dir=ROOT / "tmp") as directory:
            path = Path(directory) / "protected"
            path.write_text("safe")
            path.chmod(0o600)
            self.assertTrue(local.protected_read(path) == "safe")
            path.chmod(0o644)
            with self.assertRaises(local.SafeError):
                local.protected_read(path)
            link = Path(directory) / "link"
            link.symlink_to(path)
            with self.assertRaises(OSError):
                local.protected_read(link)

    def test_child_env_removes_admin_and_inherited_config(self):
        with patch.dict(os.environ, {"POKER_ADMIN_PASSWORD": "fixture-only", "MYSQL_PWD": "fixture-only",
                                     "POKER_ALLOW_PAID_MODELS": "true"}):
            env = local.child_env()
            self.assertNotIn("POKER_ADMIN_PASSWORD", env)
            self.assertNotIn("MYSQL_PWD", env)
            self.assertNotIn("POKER_ALLOW_PAID_MODELS", env)

    def test_safe_output_preserves_failure_and_redacts_credentials(self):
        output = io.StringIO()
        with contextlib.redirect_stdout(output):
            local.safe_output("failure context\npassword=fixture-only", {"PASSWORD": "fixture-only"})
        self.assertIn("failure context", output.getvalue())
        self.assertNotIn("fixture-only", output.getvalue())

    def test_mysql_password_only_in_child_environment(self):
        seen = {}

        def capture(argv, **kwargs):
            seen.update(argv=argv, **kwargs)
            return None

        with patch.object(local, "mysql_binary", return_value="mysql"), patch.object(local, "captured", capture):
            local.mysql("SELECT 1;", password="fixture-only")
        self.assertTrue(seen["env"]["MYSQL_PWD"] == "fixture-only")
        self.assertTrue(all("fixture-only" not in arg for arg in seen["argv"]))
        self.assertTrue(seen["data"] == "SELECT 1;")

    def test_setup_preserves_existing_env(self):
        (ROOT / "tmp").mkdir(exist_ok=True)
        with tempfile.TemporaryDirectory(dir=ROOT / "tmp") as directory:
            path = Path(directory) / ".env.local"
            path.write_text("fixture-only")
            path.chmod(0o600)

            class Result:
                returncode = 0

            with patch.object(local, "ENV_FILE", path), patch.object(local, "captured", return_value=Result()), \
                    patch.object(local, "doctor"), patch.object(local, "load_env", return_value=self.values()), \
                    contextlib.redirect_stdout(io.StringIO()):
                local.setup()
            self.assertTrue(path.read_text() == "fixture-only")

    def test_fallback_ports_allowed(self):
        values = self.values()
        values["POKER_MYSQL_DSN"] = values["POKER_MYSQL_DSN"].replace(":3306)", ":3307)")
        values["POKER_REDIS_ADDR"] = "127.0.0.1:6380"
        local.validate(values)

    def test_dev_catalog_allowed_paid_still_disabled(self):
        values = self.values()
        values["POKER_CATALOG_PATH"] = "/user-owned/model-catalog.json"
        local.validate(values, e2e=False)
        values["POKER_ALLOW_PAID_MODELS"] = "true"
        with self.assertRaises(local.SafeError):
            local.validate(values, e2e=False)

    def test_e2e_catalog_override_does_not_read_user_config(self):
        values = self.values()
        values["POKER_CATALOG_PATH"] = "/user-owned/model-catalog.json"
        text = "\n".join(k + "=" + json.dumps(v) for k, v in values.items())
        with patch.object(local, "protected_read", return_value=text), \
                patch.object(local.os.path, "lexists", return_value=True):
            result = local.load_env(e2e=True)
        self.assertTrue(result["POKER_CATALOG_PATH"] == str(local.CATALOG))

    def test_pid_identity_mismatch_rejected(self):
        class Result:
            returncode = 0
            stdout = str(os.getuid()) + " unrelated-program --other-data"

        with patch.object(local.Path, "exists", return_value=True), \
                patch.object(local, "protected_read", return_value=json.dumps(
                    {"pid": 42, "binary": "/expected/bin", "signature": "/expected/data"})), \
                patch.object(local, "captured", return_value=Result()):
            with self.assertRaises(local.SafeError):
                local.process_state("mysql")

    def test_existing_runtime_permissions_not_changed(self):
        (ROOT / "tmp").mkdir(exist_ok=True)
        with tempfile.TemporaryDirectory(dir=ROOT / "tmp") as directory:
            path = Path(directory) / "runtime"
            path.mkdir(mode=0o755)
            with self.assertRaises(local.SafeError):
                local.private_directory(path)
            self.assertTrue((path.stat().st_mode & 0o777) == 0o755)

    def identity_fixture(self, name="mysql", suffix="", forged=False, executable_wrong=False):
        binary = "/fixture/bin/" + ("mysqld" if name == "mysql" else "redis-server")
        data = str(local.RUNTIME / ("mysql-data" if name == "mysql" else "redis-data"))
        signature = "--datadir=" + data if name == "mysql" else data
        state = {"pid": 42, "binary": binary, "signature": signature + suffix if forged else signature}
        args = ([binary, "--datadir=" + data + suffix, "--port=3307", "--bind-address=127.0.0.1",
                 "--socket=" + str(local.RUNTIME / "mysql.sock"),
                 "--pid-file=" + str(local.RUNTIME / "mysql.pid")] if name == "mysql"
                else [binary, "--bind", "127.0.0.1", "--port", "6380", "--protected-mode", "yes",
                      "--dir", data + suffix, "--save", "", "--appendonly", "no",
                      "--logfile", str(local.RUNTIME / "redis-error.log")])

        def capture(argv, **kwargs):
            if argv[0] == "ps":
                return SimpleNamespace(returncode=0, stdout=str(os.getuid()) + " S Sat Oct 3 12:00:00 2026\n")
            if argv[0] == "lsof":
                path = ("/fixture/bin/unrelated" if executable_wrong else binary) if "txt" in argv else (
                    data + suffix)
                return SimpleNamespace(returncode=0, stdout="p42\nn" + path + "\n")
            return SimpleNamespace(returncode=0, stdout="/fixture\n")

        return state, capture, args, binary

    def assert_identity_rejected(self, **kwargs):
        state, capture, args, binary = self.identity_fixture(**kwargs)
        name = kwargs.get("name", "mysql")
        with patch.object(local.Path, "exists", return_value=True), \
                patch.object(local, "protected_read", return_value=json.dumps(state)), \
                patch.object(local, "mysql_binary", return_value="/fixture/bin/mysql"), \
                patch.object(local, "expected_binary", return_value=Path(binary)), \
                patch.object(local, "canonical_path", side_effect=lambda value: Path(os.path.normpath(value))), \
                patch.object(local.Path, "resolve", lambda value, **kw: value), \
                patch.object(local, "process_arguments", return_value=(binary, args)), \
                patch.object(local, "captured", side_effect=capture), \
                patch.object(local.os, "kill") as kill:
            with self.assertRaises(local.SafeError):
                local.process_state(name)
            kill.assert_not_called()

    def test_identity_mysql_path_suffix_rejected(self):
        self.assert_identity_rejected(suffix="-unrelated")

    def test_identity_forged_signature_rejected(self):
        self.assert_identity_rejected(suffix="-unrelated", forged=True)

    def test_identity_reused_pid_real_executable_rejected(self):
        self.assert_identity_rejected(executable_wrong=True)

    def test_identity_redis_similar_config_path_rejected(self):
        self.assert_identity_rejected(name="redis", suffix="-unrelated", forged=True)

    def test_identity_legal_path_with_spaces_accepted(self):
        with patch.object(local, "RUNTIME", Path("/fixture/project with spaces/.local")):
            state, capture, args, binary = self.identity_fixture()
            with patch.object(local.Path, "exists", return_value=True), \
                    patch.object(local.Path, "resolve", lambda value, **kw: value), \
                    patch.object(local, "canonical_path", side_effect=lambda value: Path(os.path.normpath(value))), \
                    patch.object(local, "expected_binary", return_value=Path(binary)), \
                    patch.object(local, "process_arguments", return_value=(binary, args)), \
                    patch.object(local, "protected_read", return_value=json.dumps(state)), \
                    patch.object(local, "captured", side_effect=capture):
                verified = local.process_state("mysql")
            self.assertTrue(verified is not None and verified["pid"] == 42)

    def test_identity_kernel_executable_spoof_rejected(self):
        state, capture, args, binary = self.identity_fixture()
        with patch.object(local.Path, "exists", return_value=True), \
                patch.object(local, "protected_read", return_value=json.dumps(state)), \
                patch.object(local, "expected_binary", return_value=Path(binary)), \
                patch.object(local, "canonical_path", side_effect=Path), \
                patch.object(local, "process_arguments", return_value=("/fixture/bin/unrelated", args)), \
                patch.object(local, "captured", side_effect=capture):
            with self.assertRaises(local.SafeError):
                local.process_state("mysql")

    def test_identity_pid_birth_metadata_reused_rejected(self):
        state, capture, args, binary = self.identity_fixture()
        state["started"] = [str(os.getuid()), "older-process-start"]
        with patch.object(local.Path, "exists", return_value=True), \
                patch.object(local, "protected_read", return_value=json.dumps(state)), \
                patch.object(local, "captured", side_effect=capture):
            with self.assertRaises(local.SafeError):
                local.process_state("mysql")

    def test_identity_unavailable_argv_rejected_no_signal(self):
        state, capture, args, binary = self.identity_fixture()
        with patch.object(local.Path, "exists", return_value=True), \
                patch.object(local, "protected_read", return_value=json.dumps(state)), \
                patch.object(local, "expected_binary", return_value=Path(binary)), \
                patch.object(local, "process_arguments", side_effect=local.SafeError("RUNTIME_ARGV_INSPECTION_FAILED")), \
                patch.object(local, "captured", side_effect=capture), patch.object(local.os, "kill") as kill:
            with self.assertRaises(local.SafeError):
                local.process_state("mysql")
            kill.assert_not_called()

    def test_stop_rechecks_identity_before_signal(self):
        first = {"pid": 42, "started": ["fixture"], "identity": ("first",)}
        changed = {"pid": 42, "started": ["new-process"], "identity": ("different",)}
        with patch.object(local.Path, "is_dir", return_value=True), \
                patch.object(local, "private_directory"), \
                patch.object(local, "process_state", side_effect=[first, changed]), \
                patch.object(local.os, "kill") as kill:
            with self.assertRaises(local.SafeError):
                local.runtime_stop()
            kill.assert_not_called()

    def test_identity_redis_similar_config_argument_rejected(self):
        state, capture, args, binary = self.identity_fixture(name="redis")
        args.insert(1, str(local.RUNTIME / "redis.conf-unrelated"))
        with patch.object(local.Path, "exists", return_value=True), \
                patch.object(local.Path, "resolve", lambda value, **kw: value), \
                patch.object(local, "canonical_path", side_effect=Path), \
                patch.object(local, "expected_binary", return_value=Path(binary)), \
                patch.object(local, "process_arguments", return_value=(binary, args)), \
                patch.object(local, "protected_read", return_value=json.dumps(state)), \
                patch.object(local, "captured", side_effect=capture), patch.object(local.os, "kill") as kill:
            with self.assertRaises(local.SafeError):
                local.process_state("redis")
            kill.assert_not_called()

    def test_identity_zombie_is_not_live(self):
        result = SimpleNamespace(returncode=0, stdout=str(os.getuid()) + " Z Sat Oct 3 12:00:00 2026\n")
        with patch.object(local, "captured", return_value=result):
            self.assertIsNone(local.process_snapshot(42))

    def test_dev_live_only_explicit_action_changes_backend_switch(self):
        values = self.values()
        with patch.object(local, "load_env", return_value=values), \
                patch.dict(os.environ, {"POKER_ALLOW_PAID_MODELS": "true"}):
            ordinary = local.action_env("dev")
            authorized = local.action_env("dev-live")
        self.assertTrue(ordinary["POKER_ALLOW_PAID_MODELS"] == "false")
        self.assertTrue(authorized["POKER_ALLOW_PAID_MODELS"] == "true")
        self.assertTrue(values["POKER_ALLOW_PAID_MODELS"] == "false")
        self.assertTrue({k for k in ordinary if ordinary[k] != authorized[k]} == {"POKER_ALLOW_PAID_MODELS"})
        # 仅后端开关变化，不构造 Web mode/budget/确认请求，不创建游戏或调用 provider。
        self.assertNotIn("mode", authorized)
        self.assertNotIn("max_live_calls", authorized)

    def test_e2e_ambient_paid_and_catalog_cannot_override(self):
        values = self.values()
        text = "\n".join(k + "=" + json.dumps(v) for k, v in values.items())
        with patch.object(local, "protected_read", return_value=text), \
                patch.object(local.os.path, "lexists", return_value=True), \
                patch.dict(os.environ, {"POKER_ALLOW_PAID_MODELS": "true", "POKER_CATALOG_PATH": "/untrusted/catalog"}):
            env = local.child_env(local.load_env(e2e=True))
        self.assertTrue(env["POKER_ALLOW_PAID_MODELS"] == "false")
        self.assertTrue(env["POKER_CATALOG_PATH"] == str(local.CATALOG))

    def test_redaction_does_not_consume_next_test_line(self):
        text = "=== RUN TestLiveGateAndBudget/dual_authorization\n=== RUN TestNext\n"
        self.assertEqual(local.sanitize_output(text), text)

    def test_probe_resolved_port_in_use_refuses_without_signal(self):
        payload = json.loads(self.config_payload(port=8083))
        with patch.object(local, "config_public", return_value=payload), \
                patch.object(local, "port_open", return_value=True) as probe, \
                patch.object(local.subprocess, "Popen") as launch, patch.object(local.os, "kill") as kill:
            with self.assertRaises(local.SafeError):
                local.probe_dev()
            probe.assert_called_once_with(8083)
            launch.assert_not_called()
            kill.assert_not_called()

    def test_config_public_honors_explicit_env_override(self):
        seen = {}

        def runner(argv, **kwargs):
            seen.update(kwargs)
            return SimpleNamespace(returncode=0, stdout=self.config_payload(port=8083))

        result = local.config_public(runner=runner, env={"POKER_CONFIG_FILE": "/tmp/override.yaml", "PATH": "/bin"})
        self.assertTrue(seen["env"]["POKER_CONFIG_FILE"] == "/tmp/override.yaml")
        self.assertTrue(result["http_port"] == 8083)

    def test_service_env_injects_absolute_config_file(self):
        values = self.values()
        with patch.dict(os.environ, {"POKER_CONFIG_FILE": "relative/config.yaml"}):
            relative = local.service_env(values, action="dev")
        self.assertTrue(os.path.isabs(relative["POKER_CONFIG_FILE"]))
        saved = os.environ.pop("POKER_CONFIG_FILE", None)
        try:
            default = local.service_env(values, action="dev")
        finally:
            if saved is not None:
                os.environ["POKER_CONFIG_FILE"] = saved
        self.assertTrue(os.path.isabs(default["POKER_CONFIG_FILE"]))
        self.assertTrue(default["POKER_CONFIG_FILE"].endswith(os.path.join("config", "config.yaml")))

    def test_go_contract_check_rejects_run_without_match(self):
        self.assertFalse(local._go_contract_check("ok  \ttexas-holdem/internal/conf\t0.1s [no tests to run]"))
        self.assertFalse(local._go_contract_check("ok  \ttexas-holdem/internal/conf\t0.1s"))
        self.assertTrue(local._go_contract_check("=== RUN   TestX\n--- PASS: TestX (0.00s)"))

    # --- make run：一键先编译前端（生成最新 dist）再前台 dev 启动 ---

    def test_run_compiles_frontend_before_starting_dev(self):
        calls = []
        with patch.object(local, "frontend", side_effect=lambda: calls.append("frontend")), \
                patch.object(local, "go_task", side_effect=lambda action: calls.append(action) or 0):
            result = local.run()
        self.assertEqual(calls, ["frontend", "dev"])
        self.assertEqual(result, 0)

    def test_run_frontend_failure_stops_before_service(self):
        with patch.object(local, "frontend",
                          side_effect=local.SafeError("FRONTEND_STAGE_FAILED_FRONTEND_BUILD")), \
                patch.object(local, "go_task") as go:
            with self.assertRaises(local.SafeError):
                local.run()
        go.assert_not_called()

    def test_main_run_action_dispatches_to_run(self):
        with patch.object(local.sys, "argv", ["local.py", "run"]), \
                patch.object(local, "run", return_value=7) as run:
            self.assertEqual(local.main(), 7)
        run.assert_called_once_with()

    # --- 配置 CLI 契约（端口/优先级/env 安全）：冻结 Go CLI 消费侧 RED 测试 ---

    def config_payload(self, host="127.0.0.1", port=8083, frontend=5173, extra=None):
        data = {"http_base_url": "http://" + host + ":" + str(port),
                "http_address": host + ":" + str(port), "http_host": host,
                "http_port": port, "frontend_port": frontend}
        if extra:
            data.update(extra)
        return json.dumps(data)

    def test_config_public_derives_endpoint_from_cli_not_constant(self):
        runner = lambda *a, **k: SimpleNamespace(returncode=0, stdout=self.config_payload())
        result = local.config_public(runner=runner)
        self.assertTrue(result["http_port"] == 8083)
        self.assertTrue(local.config_endpoint(runner=runner) == "http://127.0.0.1:8083")

    def test_config_public_rejects_non_whitelist_and_secret_fields(self):
        payload = self.config_payload(extra={"mysql_dsn": "fixture-only"})
        with self.assertRaises(local.SafeError):
            local.config_public(runner=lambda *a, **k: SimpleNamespace(returncode=0, stdout=payload))

    def test_config_public_rejects_non_loopback_host(self):
        payload = self.config_payload(host="0.0.0.0")
        with self.assertRaises(local.SafeError):
            local.config_public(runner=lambda *a, **k: SimpleNamespace(returncode=0, stdout=payload))

    def test_config_public_requires_consistent_address(self):
        payload = json.dumps({"http_base_url": "http://127.0.0.1:9999", "http_address": "127.0.0.1:8083",
                              "http_host": "127.0.0.1", "http_port": 8083, "frontend_port": 5173})
        with self.assertRaises(local.SafeError):
            local.config_public(runner=lambda *a, **k: SimpleNamespace(returncode=0, stdout=payload))

    def test_config_cli_failure_is_blocker_not_default(self):
        with self.assertRaises(local.SafeError):
            local.config_public(runner=lambda *a, **k: SimpleNamespace(returncode=1, stdout=""))

    def test_service_env_e2e_forces_mock_and_false_and_drops_legacy_nonsensitive(self):
        values = self.values()
        with patch.dict(os.environ, {"POKER_REDIS_ADDR": "127.0.0.1:6379", "POKER_ALLOW_PAID_MODELS": "true",
                                     "POKER_CATALOG_PATH": "/untrusted/catalog"}):
            env = local.service_env(values, e2e=True)
        self.assertTrue(env["POKER_CATALOG_PATH"] == str(local.CATALOG))
        self.assertTrue(env["POKER_ALLOW_PAID_MODELS"] == "false")
        # 旧非敏感 RedisAddr 与旧 DSN/密码均不再从 .env.local 注入（改由 YAML 直连）。
        self.assertNotIn("POKER_REDIS_ADDR", env)
        self.assertNotIn("POKER_MYSQL_DSN", env)
        self.assertNotIn("POKER_REDIS_PASSWORD", env)

    def test_validate_tolerates_empty_or_near_empty_env(self):
        local.validate({})
        local.validate({}, e2e=False)

    def test_storage_config_merges_direct_fields_over_env(self):
        (ROOT / "tmp").mkdir(exist_ok=True)
        with tempfile.TemporaryDirectory(dir=ROOT / "tmp") as directory:
            base = Path(directory) / "config.yaml"
            base.write_text(
                "data:\n  mysql:\n    dsn_env: POKER_MYSQL_DSN\n"
                "  redis:\n    addr: 127.0.0.1:6379\n    db: 0\n    password_env: POKER_REDIS_PASSWORD\n")
            (Path(directory) / "config.local.yaml").write_text(
                "data:\n  mysql:\n    dsn: \"app:fixture-secret@tcp(127.0.0.1:3306)/texas_holdem?parseTime=true\"\n"
                "  redis:\n    addr: 127.0.0.1:6379\n    db: 3\n    password: \"fixture-redis-secret\"\n")
            with patch.dict(os.environ, {"POKER_MYSQL_DSN": "env:ignored@tcp(127.0.0.1:3306)/x?parseTime=true"}):
                conn = local.storage_config(base=base)
        self.assertTrue(conn["mysql_port"] == 3306 and conn["mysql_user"] == "app")
        self.assertTrue(conn["mysql_database"] == "texas_holdem")
        self.assertTrue(conn["mysql_password"] == "fixture-secret")
        self.assertTrue(conn["redis_port"] == 6379 and conn["redis_db"] == 3)
        self.assertTrue(conn["redis_password"] == "fixture-redis-secret")

    def test_load_env_accepts_near_empty_file(self):
        with patch.object(local, "protected_read", return_value="# storage lives in config.local.yaml\n"), \
                patch.object(local.os.path, "lexists", return_value=True), \
                patch.dict(os.environ, {"POKER_CATALOG_PATH": ""}):
            self.assertEqual(local.load_env(), {})

    def test_service_env_dev_forwards_whitelist_and_ignores_ambient_paid(self):
        values = self.values()
        with patch.dict(os.environ, {"POKER_HTTP_PORT": "8083", "POKER_CONFIG_FILE": "/tmp/fixture.yaml",
                                     "POKER_ALLOW_PAID_MODELS": "true"}):
            env = local.service_env(values, action="dev")
        self.assertTrue(env["POKER_HTTP_PORT"] == "8083")
        self.assertTrue(env["POKER_CONFIG_FILE"] == "/tmp/fixture.yaml")
        self.assertTrue(env["POKER_ALLOW_PAID_MODELS"] == "false")
        self.assertNotIn("POKER_REDIS_ADDR", env)

    def test_service_env_dev_live_only_explicit_action_enables_paid(self):
        values = self.values()
        ordinary = local.service_env(values, action="dev")
        live = local.service_env(values, action="dev-live")
        self.assertTrue(ordinary["POKER_ALLOW_PAID_MODELS"] == "false")
        self.assertTrue(live["POKER_ALLOW_PAID_MODELS"] == "true")

    def test_config_cli_env_only_forwards_whitelist(self):
        with patch.dict(os.environ, {"POKER_HTTP_PORT": "8083", "POKER_CONFIG_FILE": "/tmp/fixture.yaml",
                                     "POKER_MYSQL_DSN": "fixture-dsn", "POKER_REDIS_PASSWORD": "fixture-secret",
                                     "POKER_ALLOW_PAID_MODELS": "true"}):
            env = local.config_cli_env()
        self.assertTrue(env["POKER_HTTP_PORT"] == "8083")
        self.assertTrue(env["POKER_CONFIG_FILE"] == "/tmp/fixture.yaml")
        for secret in ("POKER_MYSQL_DSN", "POKER_REDIS_PASSWORD", "POKER_ALLOW_PAID_MODELS"):
            self.assertNotIn(secret, env)

    # --- bug 2：Python 存储解析与 Go 契约一致（字段存在性决定回退） ---

    def storage_fixture(self, directory, local_text):
        base = Path(directory) / "config.yaml"
        base.write_text("data:\n  mysql:\n    dsn: \"app:fixture-dsn@tcp(127.0.0.1:3306)/texas_holdem?parseTime=true\"\n"
                        "  redis:\n    addr: 127.0.0.1:6379\n    db: 0\n    password_env: POKER_REDIS_PASSWORD\n")
        (Path(directory) / "config.local.yaml").write_text(local_text)
        return base

    def test_storage_config_explicit_empty_password_ignores_env(self):
        (ROOT / "tmp").mkdir(exist_ok=True)
        with tempfile.TemporaryDirectory(dir=ROOT / "tmp") as directory:
            base = self.storage_fixture(directory, 'data:\n  redis:\n    password: ""\n')
            with patch.dict(os.environ, {"POKER_REDIS_PASSWORD": "fixture-env-secret"}):
                conn = local.storage_config(base=base)
            self.assertTrue(conn["redis_password"] == "")

    def test_storage_config_absent_password_falls_back_to_env(self):
        (ROOT / "tmp").mkdir(exist_ok=True)
        with tempfile.TemporaryDirectory(dir=ROOT / "tmp") as directory:
            base = self.storage_fixture(directory, "data:\n  redis:\n    db: 0\n")
            with patch.dict(os.environ, {"POKER_REDIS_PASSWORD": "fixture-env-secret"}):
                conn = local.storage_config(base=base)
            self.assertTrue(conn["redis_password"] == "fixture-env-secret")

    def test_storage_config_null_password_falls_back_to_env(self):
        (ROOT / "tmp").mkdir(exist_ok=True)
        with tempfile.TemporaryDirectory(dir=ROOT / "tmp") as directory:
            base = self.storage_fixture(directory, "data:\n  redis:\n    password:\n    db: 0\n")
            with patch.dict(os.environ, {"POKER_REDIS_PASSWORD": "fixture-env-secret"}):
                conn = local.storage_config(base=base)
            self.assertTrue(conn["redis_password"] == "fixture-env-secret")

    # --- bug 3：doctor 实际校验受保护文件权限后才能下结论 ---

    def test_protected_stat_rejects_wrong_mode(self):
        (ROOT / "tmp").mkdir(exist_ok=True)
        with tempfile.TemporaryDirectory(dir=ROOT / "tmp") as directory:
            path = Path(directory) / "protected"
            path.write_text("x")
            path.chmod(0o644)
            with self.assertRaises(local.SafeError):
                local.protected_stat(path, "ENV_PERMISSION_INVALID")
            path.chmod(0o600)
            local.protected_stat(path, "ENV_PERMISSION_INVALID")

    def test_doctor_rejects_env_file_without_private_mode(self):
        (ROOT / "tmp").mkdir(exist_ok=True)
        with tempfile.TemporaryDirectory(dir=ROOT / "tmp") as directory:
            env_file = Path(directory) / ".env.local"
            env_file.write_text("# comment-only\n")
            env_file.chmod(0o644)
            with patch.object(local, "ENV_FILE", env_file), \
                    patch.object(local, "storage_config", return_value={}), \
                    patch.object(local, "mysql_query", return_value=SimpleNamespace(returncode=0, stdout="1")), \
                    patch.object(local, "redis_ping"):
                with self.assertRaises(local.SafeError):
                    local.doctor()

    # --- bug 1：setup 凭据交接（写入 config.local.yaml，保留已有不覆盖） ---

    def test_setup_writes_generated_credentials_to_local_config(self):
        (ROOT / "tmp").mkdir(exist_ok=True)
        with tempfile.TemporaryDirectory(dir=ROOT / "tmp") as directory:
            base = Path(directory) / "config.yaml"
            base.write_text("server:\n  http:\n    port: 8081\n")
            env_file = Path(directory) / "envdir" / ".env.local"
            env_file.parent.mkdir()
            results = [SimpleNamespace(returncode=0, stdout="0\n0\n"),
                       SimpleNamespace(returncode=0, stdout="")]
            with patch.object(local, "config_file_path", return_value=base), \
                    patch.object(local, "ENV_FILE", env_file), \
                    patch.object(local, "captured", return_value=SimpleNamespace(returncode=0, stdout="")), \
                    patch.object(local, "port_open", return_value=True), \
                    patch.object(local, "mysql", side_effect=lambda *a, **k: results.pop(0)), \
                    patch.object(local, "redis_ping"), \
                    patch.object(local, "doctor") as doctor, \
                    patch.object(local.time, "sleep"):
                local.setup()
            local_config = Path(directory) / "config.local.yaml"
            self.assertTrue(local_config.is_file())
            text = local_config.read_text()
            self.assertIn("dsn:", text)
            self.assertIn("texas_holdem_app:", text)
            self.assertIn("password:", text)
            self.assertTrue(env_file.is_file())
            self.assertNotIn("POKER_MYSQL_DSN", env_file.read_text())
            conn = local.storage_config(base=base)
            self.assertTrue(conn["mysql_user"] == "texas_holdem_app")
            self.assertTrue(conn["redis_password"] == "")
            doctor.assert_called_once()

    def test_setup_preserves_existing_local_config(self):
        (ROOT / "tmp").mkdir(exist_ok=True)
        sentinel = 'data:\n  mysql:\n    dsn: "sentinel:keep@tcp(127.0.0.1:3306)/keep?parseTime=true"\n'
        with tempfile.TemporaryDirectory(dir=ROOT / "tmp") as directory:
            base = Path(directory) / "config.yaml"
            base.write_text("server:\n  http:\n    port: 8081\n")
            (Path(directory) / "config.local.yaml").write_text(sentinel)
            env_file = Path(directory) / "envdir" / ".env.local"
            env_file.parent.mkdir()
            with patch.object(local, "config_file_path", return_value=base), \
                    patch.object(local, "ENV_FILE", env_file), \
                    patch.object(local, "captured", return_value=SimpleNamespace(returncode=0, stdout="")), \
                    patch.object(local, "mysql") as mysql, \
                    patch.object(local, "doctor") as doctor:
                local.setup()
            self.assertTrue((Path(directory) / "config.local.yaml").read_text() == sentinel)
            mysql.assert_not_called()
            doctor.assert_called_once()


if __name__ == "__main__":
    # 不输出失败 traceback，避免将未来新增配置测试中的值带入终端。
    suite = unittest.defaultTestLoader.loadTestsFromTestCase(LocalSafety)
    result = unittest.TestResult()
    suite.run(result)
    failed = {test.id().split(".")[-1] for test, _ in result.failures + result.errors}
    names = sorted(name for name in dir(LocalSafety) if name.startswith("test_"))
    for name in names:
        print(name + ": " + ("FAIL" if name in failed else "PASS"))
    for test, details in result.failures + result.errors:
        local.safe_output(details)
    print("scripts: tests=" + str(result.testsRun) + " passed=" +
          str(result.testsRun - len(failed)) + " failed=" + str(len(failed)))
    raise SystemExit(0 if result.wasSuccessful() else 1)
