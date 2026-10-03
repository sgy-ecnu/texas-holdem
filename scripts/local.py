#!/usr/bin/env python3
"""本地限定工具：配置从受保护文件进入子进程环境，不经 shell/argv。"""
import json
import ctypes
import os
from pathlib import Path
import plistlib
import re
import secrets
import signal
import socket
import stat
import subprocess
import sys
import tempfile
import threading
import time
import urllib.error
import urllib.request

ROOT = Path(__file__).resolve().parents[1]
ENV_FILE = ROOT / ".env.local"
CATALOG = ROOT / "scripts/mock-catalog.json"
KEYS = {"POKER_MYSQL_DSN", "POKER_REDIS_ADDR", "POKER_REDIS_PASSWORD",
        "POKER_REDIS_DB", "POKER_CATALOG_PATH", "POKER_ALLOW_PAID_MODELS"}
DSN = re.compile(r"texas_holdem_app:([A-Za-z0-9_-]{32,})@tcp\(127\.0\.0\.1:(3306|3307)\)/texas_holdem\?parseTime=true")
RUNTIME = ROOT / ".local"
# 冻结配置 CLI：唯一公共 JSON 契约，不依赖 web/dist，不输出 secret。
CONFIG_PACKAGE = "./cmd/config"
CONFIG_EVIDENCE = ROOT / "docs/CONFIG_REFACTOR_VERIFICATION.md"
CONFIG_PUBLIC_KEYS = {"http_base_url", "http_address", "http_host", "http_port", "frontend_port"}
# 显式白名单 env 覆盖：只转发这些非敏感键，绝不转发 DSN/Redis 密码等凭据。
CONFIG_ENV_WHITELIST = ("POKER_CONFIG_FILE", "POKER_HTTP_HOST", "POKER_HTTP_PORT",
                        "POKER_HTTP_TIMEOUT", "POKER_FRONTEND_PORT")


class SafeError(Exception):
    """仅允许固定、无配置值的错误码。"""


def require(ok, code):
    if not ok:
        raise SafeError(code)


def protected_read(path):
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
    with os.fdopen(fd, "r", encoding="utf-8") as f:
        s = os.fstat(f.fileno())
        require(stat.S_ISREG(s.st_mode) and stat.S_IMODE(s.st_mode) == 0o600
                and s.st_uid == os.getuid(), "ENV_PERMISSION_INVALID")
        text = f.read(65537)
        require(len(text) <= 65536, "ENV_TOO_LARGE")
        return text


def protected_stat(path, code):
    """打开受保护文件并仅核验类型/权限/owner，不读取内容、不输出值。"""
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
    try:
        s = os.fstat(fd)
        require(stat.S_ISREG(s.st_mode) and stat.S_IMODE(s.st_mode) == 0o600
                and s.st_uid == os.getuid(), code)
    finally:
        os.close(fd)


def parse_env(text):
    values = {}
    for line in text.splitlines():
        if not line.strip() or line.lstrip().startswith("#"):
            continue
        key, sep, raw = line.partition("=")
        require(sep and key in KEYS and key not in values, "ENV_FORMAT_INVALID")
        # JSON 字符串，不执行 source、变量展开或命令替换。
        value = json.loads(raw)
        require(isinstance(value, str) and not any(c in value for c in "\r\n\x00"),
                "ENV_FORMAT_INVALID")
        values[key] = value
    return values


def validate(values, e2e=True):
    # 只校验「存在」的键：清理旧隔离凭据后 .env.local 可为空，不得因缺键报错。
    if "POKER_MYSQL_DSN" in values:
        require(DSN.fullmatch(values["POKER_MYSQL_DSN"]) is not None, "ENV_MYSQL_NOT_LOCAL")
    if "POKER_REDIS_ADDR" in values:
        require(values["POKER_REDIS_ADDR"] in {"127.0.0.1:6379", "127.0.0.1:6380"}, "ENV_REDIS_NOT_LOCAL")
    if "POKER_REDIS_DB" in values:
        require(values["POKER_REDIS_DB"] == "15", "ENV_REDIS_DB_INVALID")
    if "POKER_CATALOG_PATH" in values:
        if e2e:
            require(values["POKER_CATALOG_PATH"] == str(CATALOG), "ENV_CATALOG_NOT_MOCK")
        else:
            require(Path(values["POKER_CATALOG_PATH"]).is_absolute(), "ENV_CATALOG_PATH_INVALID")
    if "POKER_ALLOW_PAID_MODELS" in values:
        require(values["POKER_ALLOW_PAID_MODELS"] == "false", "ENV_PAID_MODELS_FORBIDDEN")


def load_env(e2e=False):
    require(os.path.lexists(ENV_FILE), "ENV_LOCAL_MISSING")
    values = parse_env(protected_read(ENV_FILE))
    if e2e:
        values["POKER_CATALOG_PATH"] = str(CATALOG)
    else:
        catalog = os.environ.get("POKER_CATALOG_PATH")
        default = Path.home() / ".config/opencode/opencode.jsonc"
        if catalog:
            values["POKER_CATALOG_PATH"] = catalog
        elif values.get("POKER_CATALOG_PATH") == str(CATALOG):
            if default.is_file():
                values["POKER_CATALOG_PATH"] = str(default)
            elif default.with_suffix(".json").is_file():
                values["POKER_CATALOG_PATH"] = str(default.with_suffix(".json"))
    validate(values, e2e=e2e)
    return values


def child_env(values=None):
    env = {k: v for k, v in os.environ.items()
           if not k.startswith(("POKER_", "MYSQL")) and k not in {"BASH_ENV", "ENV"}}
    env.update(values or {})
    env["PYTHONDONTWRITEBYTECODE"] = "1"
    return env


def config_file_path():
    """服务子进程使用的绝对 base 配置路径。

    绝对化以确保嵌入二进制在任意 cwd（含 E2E 临时目录）解析到同一 YAML；不读取其内容。
    """
    raw = os.environ.get("POKER_CONFIG_FILE") or str(ROOT / "config/config.yaml")
    path = Path(raw).expanduser()
    if not path.is_absolute():
        path = Path.cwd() / path
    return path


DSN_RE = re.compile(
    r"^(?P<user>[^:@/]+)(?::(?P<password>[^@]*))?@tcp\((?P<host>[^:()]+):(?P<port>\d+)\)/(?P<database>[^?]+)")


def _parse_scalar(raw):
    raw = raw.strip()
    if len(raw) >= 2 and raw[0] == raw[-1] and raw[0] in "\"'":
        return raw[1:-1]
    return raw


def _read_yaml_subset(path):
    """极简 YAML 读取：仅支持本项目配置的嵌套映射 + 标量；只解析，不输出任何值。

    空值语义与 Go 对齐：`key:`（null）记为 None（等同未设置）；
    `key: ""`（显式空串）记为 ""。两者语义不同，不可混同。
    """
    require(path.is_file(), "CONFIG_READ_FAILED")
    parsed = []
    for line in path.read_text(encoding="utf-8").splitlines():
        stripped = line.strip()
        if not stripped or stripped.startswith("#"):
            continue
        parsed.append((len(line) - len(line.lstrip(" ")), stripped))
    root = {}
    stack = [(-1, root)]
    for index, (indent, stripped) in enumerate(parsed):
        key, sep, raw = stripped.partition(":")
        require(sep and key.strip() and not key.strip().startswith("-"), "CONFIG_INVALID")
        key = key.strip()
        while len(stack) > 1 and indent <= stack[-1][0]:
            stack.pop()
        parent = stack[-1][1]
        if raw.strip() != "":
            parent[key] = _parse_scalar(raw)
            continue
        child = bool(parsed[index + 1:]) and parsed[index + 1][0] > indent
        if child:
            node = {}
            parent[key] = node
            stack.append((indent, node))
        else:
            parent[key] = None
    return root


def storage_config(base=None):
    """合并 base YAML 与同目录 config.local.yaml 的 data 段，返回内存字典。

    直连字段（data.mysql.dsn / data.redis.password）优先于对应 env。返回值含 secret，
    只允许在本地进程内用于连接，绝不打印、绝不写入日志或错误码。
    """
    base = base or config_file_path()
    merged = {}
    for section, kv in _read_yaml_subset(base).get("data", {}).items():
        if isinstance(kv, dict):
            merged[section] = dict(kv)
    local = base.parent / "config.local.yaml"
    if local.is_file():
        for section, kv in _read_yaml_subset(local).get("data", {}).items():
            if isinstance(kv, dict):
                merged.setdefault(section, {}).update(kv)
    mysql = merged.get("mysql", {})
    redis = merged.get("redis", {})
    dsn = mysql.get("dsn") or os.environ.get(mysql.get("dsn_env", "POKER_MYSQL_DSN"), "")
    require(isinstance(dsn, str) and bool(dsn), "CONFIG_STORAGE_REQUIRED")
    match = DSN_RE.match(dsn)
    require(match is not None, "CONFIG_DSN_INVALID")
    assert match is not None
    addr = str(redis.get("addr", ""))
    host, sep, port = addr.rpartition(":")
    require(sep and host == "127.0.0.1" and port.isdigit(), "CONFIG_REDIS_NOT_LOCAL")
    db_raw = str(redis.get("db", "0"))
    require(db_raw.isdigit(), "CONFIG_REDIS_DB_INVALID")
    # 与 Go 一致：password 字段「存在性」决定是否回退（显式空串 = 无密码，不回退 env；
    # null/缺省才读 password_env）。
    if "password" in redis and redis["password"] is not None:
        password = redis["password"]
    else:
        password = os.environ.get(redis.get("password_env") or "POKER_REDIS_PASSWORD", "")
    return {
        "mysql_host": match.group("host"), "mysql_port": int(match.group("port")),
        "mysql_user": match.group("user"), "mysql_password": match.group("password") or "",
        "mysql_database": match.group("database"),
        "redis_host": host, "redis_port": int(port), "redis_db": int(db_raw),
        "redis_password": password or "",
    }


def service_env(values=None, *, action=None, e2e=False, explicit=None):
    """服务/Go 子进程环境：仅注入凭据、白名单覆盖与绝对 POKER_CONFIG_FILE。

    旧 .env.local 的非敏感值（RedisAddr/DB/Catalog/paid）不再默认注入，交 YAML 决定；
    E2E 强制 mock + paid=false 最高优先；dev-live 仅显式 action 打开付费；
    explicit 供 E2E 显式注入测试端点（如 Redis 代理端口），在强制项之前应用。
    """
    overrides = {key: os.environ[key] for key in CONFIG_ENV_WHITELIST if key in os.environ}
    env = child_env(overrides)
    # 不再从 .env.local 注入旧 DSN/Redis 密码：改由 config.local.yaml 直连字段提供，
    # 且直连优先级高于 env。explicit 仍供 E2E 显式注入测试端点（如失败注入）。
    # 绝对配置路径：嵌入二进制可在任意 cwd（含 E2E 临时目录）解析到同一 base YAML。
    env["POKER_CONFIG_FILE"] = str(config_file_path())
    for key, value in (explicit or {}).items():
        env[key] = value
    if e2e:
        env["POKER_CATALOG_PATH"] = str(CATALOG)
        env["POKER_ALLOW_PAID_MODELS"] = "false"
        return env
    catalog = os.environ.get("POKER_CATALOG_PATH")
    if catalog:
        env["POKER_CATALOG_PATH"] = catalog
    env["POKER_ALLOW_PAID_MODELS"] = "true" if action == "dev-live" else "false"
    return env


def captured(argv, *, env=None, data=None, timeout=120):
    # 所有子进程输出先留在进程内，不转发原始错误/日志。
    return subprocess.run(argv, cwd=ROOT, env=env or child_env(), input=data,
                          stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                          text=True, timeout=timeout)


def config_cli_env():
    """调用配置 CLI 的最小环境：剥离全部 POKER_*/MYSQL* 凭据，仅回填非敏感白名单覆盖。"""
    overrides = {key: os.environ[key] for key in CONFIG_ENV_WHITELIST if key in os.environ}
    return child_env(overrides)


def config_public(runner=None, env=None):
    """调用冻结 Go CLI 取白名单 JSON；失败即阻塞，绝不回退到硬编码端口默认值。"""
    run = runner or captured
    result = run(["go", "run", CONFIG_PACKAGE], env=env or config_cli_env(), timeout=180)
    require(result.returncode == 0, "CONFIG_CLI_FAILED")
    text = (result.stdout or "").strip()
    require(bool(text) and len(text) <= 4096, "CONFIG_OUTPUT_INVALID")
    try:
        data = json.loads(text)
    except json.JSONDecodeError:
        raise SafeError("CONFIG_OUTPUT_INVALID")
    require(isinstance(data, dict) and set(data) == CONFIG_PUBLIC_KEYS, "CONFIG_OUTPUT_FIELDS_INVALID")
    host = data.get("http_host")
    port = data.get("http_port")
    frontend = data.get("frontend_port")
    require(isinstance(host, str) and host in {"127.0.0.1", "localhost"}, "CONFIG_HTTP_HOST_NOT_LOCAL")
    require(type(port) is int and 1 <= port <= 65535, "CONFIG_HTTP_PORT_INVALID")
    require(type(frontend) is int and 1 <= frontend <= 65535, "CONFIG_FRONTEND_PORT_INVALID")
    address = str(host) + ":" + str(port)
    require(data.get("http_address") == address, "CONFIG_HTTP_ADDRESS_INVALID")
    require(data.get("http_base_url") == "http://" + address, "CONFIG_HTTP_BASE_URL_INVALID")
    return data


def config_endpoint(runner=None):
    return config_public(runner=runner)["http_base_url"]


def mysql_binary():
    p = captured(["brew", "--prefix", "mysql@8.4"])
    require(p.returncode == 0, "MYSQL_CLIENT_UNAVAILABLE")
    path = Path(p.stdout.strip()) / "bin/mysql"
    require(path.is_file(), "MYSQL_CLIENT_UNAVAILABLE")
    return str(path)


def mysql(sql, *, password=None, admin=False, port=3306, runtime=False,
          host="127.0.0.1", user="texas_holdem_app", database="texas_holdem"):
    env = child_env()
    env["MYSQL_HISTFILE"] = "/dev/null"
    args = [mysql_binary(), "--no-defaults", "--batch", "--skip-column-names",
            "--connect-timeout=5"]
    if admin:
        admin_user = "root" if runtime else os.environ.get("POKER_ADMIN_USER", "root")
        sock = str(RUNTIME / "mysql.sock") if runtime else os.environ.get("POKER_ADMIN_SOCKET", "/tmp/mysql.sock")
        require(re.fullmatch(r"[A-Za-z0-9_]+", admin_user), "ADMIN_USER_INVALID")
        require(sock in {"/tmp/mysql.sock", "/opt/homebrew/var/mysql/mysql.sock",
                             "/usr/local/var/mysql/mysql.sock", str(RUNTIME / "mysql.sock")}, "ADMIN_SOCKET_NOT_LOCAL")
        args += ["--protocol=SOCKET", "--socket=" + sock, "--user=" + admin_user]
        password = None if runtime else os.environ.get("POKER_ADMIN_PASSWORD")
    else:
        require(host == "127.0.0.1", "MYSQL_HOST_NOT_LOCAL")
        require(port in {3306, 3307}, "MYSQL_PORT_NOT_LOCAL")
        require(re.fullmatch(r"[A-Za-z0-9_]+", user), "MYSQL_USER_INVALID")
        require(re.fullmatch(r"[A-Za-z0-9_]+", database), "MYSQL_DATABASE_INVALID")
        args += ["--protocol=TCP", "--host=" + host, "--port=" + str(port),
                 "--user=" + user, "--database=" + database]
    if password is not None:
        env["MYSQL_PWD"] = password
    return captured(args, env=env, data=sql, timeout=20)


def mysql_query(storage, sql):
    """用 storage_config() 解析出的连接执行只读/迁移查询；凭据只在子进程环境。"""
    return mysql(sql, password=storage["mysql_password"], port=storage["mysql_port"],
                 host=storage["mysql_host"], user=storage["mysql_user"],
                 database=storage["mysql_database"])


def redis_ping(storage):
    # RESP 直连 loopback；认证值只在内存/socket，不传 redis-cli argv。
    def command(s, *parts):
        items = [p.encode() for p in parts]
        s.sendall(b"*" + str(len(items)).encode() + b"\r\n" + b"".join(
            b"$" + str(len(p)).encode() + b"\r\n" + p + b"\r\n" for p in items))
        result = b""
        while not result.endswith(b"\r\n"):
            block = s.recv(1024)
            require(block and len(result) < 4096, "REDIS_PROTOCOL_INVALID")
            result += block
        return result
    host = storage.get("redis_host", "127.0.0.1")
    port = storage["redis_port"]
    require(host == "127.0.0.1" and port in {6379, 6380}, "REDIS_PORT_NOT_LOCAL")
    with socket.create_connection((host, port), timeout=3) as s:
        if storage.get("redis_password"):
            require(command(s, "AUTH", storage["redis_password"]) == b"+OK\r\n",
                    "REDIS_AUTH_FAILED")
        require(command(s, "SELECT", str(storage["redis_db"])) == b"+OK\r\n", "REDIS_SELECT_FAILED")
        require(command(s, "PING") == b"+PONG\r\n", "REDIS_PING_FAILED")


def doctor(e2e=False):
    require(os.path.lexists(ENV_FILE), "ENV_LOCAL_MISSING")
    # 只有实际核验类型/权限/owner 后才输出结论，不谎报安全保证。
    protected_stat(ENV_FILE, "ENV_PERMISSION_INVALID")
    print("env: exists; regular-file; owner=current-user; mode=600")
    local_config = config_file_path().parent / "config.local.yaml"
    if local_config.is_file():
        protected_stat(local_config, "CONFIG_LOCAL_PERMISSION_INVALID")
        print("config-local: exists; regular-file; owner=current-user; mode=600")
    else:
        print("config-local: missing")
    # 存储连接来自 config.yaml + config.local.yaml 直连字段；不由 .env.local 提供。
    conn = storage_config()
    p = mysql_query(conn, "SELECT 1;\n")
    require(p.returncode == 0 and p.stdout.strip() == "1", "APP_MYSQL_PROBE_FAILED")
    print("mysql: available; loopback:" + str(conn["mysql_port"]) + "; db-configured")
    redis_ping(conn)
    print("redis: available; loopback:" + str(conn["redis_port"]) + "; db=" + str(conn["redis_db"]) + "; no writes")
    print("frontend-dist: " + ("present" if (ROOT / "web/dist/index.html").is_file() else "missing"))
    print("checks: passed=3 failed=0")


def _ensure_env_file():
    """.env.local 仅需存在（load_env 有 ENV_LOCAL_MISSING 检查）；不存放存储凭据。"""
    if os.path.lexists(ENV_FILE):
        return
    fd = os.open(ENV_FILE, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, "w", encoding="utf-8") as f:
        f.write("# storage credentials live in config/config.local.yaml\n")
        f.flush()
        os.fsync(f.fileno())


def _write_local_config(path, dsn, redis_addr, redis_db, redis_password):
    """O_EXCL 写入直连凭据（mode 600）；调用方须先确认不存在，避免覆盖用户真实凭据。"""
    body = ("# generated by scripts/local.py setup; local direct credentials (mode 600, gitignored)\n"
            "data:\n"
            "  mysql:\n"
            "    dsn: " + json.dumps(dsn) + "\n"
            "  redis:\n"
            "    addr: " + json.dumps(redis_addr) + "\n"
            "    db: " + str(redis_db) + "\n"
            "    password: " + json.dumps(redis_password) + "\n")
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, "w", encoding="utf-8") as f:
        f.write(body)
        f.flush()
        os.fsync(f.fileno())


def setup():
    # 非 Git 工作目录也验证忽略规则；若存在 Git，再检查真实 ignore 状态。
    rules = (ROOT / ".gitignore").read_text().splitlines()
    require(".env.*" in rules and "!.env.local" not in rules, "ENV_IGNORE_REQUIRED")
    git = captured(["git", "rev-parse", "--is-inside-work-tree"])
    if git.returncode == 0:
        require(captured(["git", "check-ignore", "-q", ".env.local"]).returncode == 0,
                "ENV_IGNORE_REQUIRED")
    else:
        print("git: unavailable; checked .gitignore rules only")
    local_config = config_file_path().parent / "config.local.yaml"
    if local_config.is_file():
        # 凭据权威来源为 config.local.yaml；已存在则保留、绝不覆盖用户真实凭据。
        print("config-local: already-present; not overwritten")
        _ensure_env_file()
        doctor()
        return
    for service in ("mysql@8.4", "redis"):
        p = captured(["brew", "services", "start", service])
        print("brew-service: " + service + (" start requested" if p.returncode == 0 else " launch failed"))
    time.sleep(2)
    fallback = not (port_open(3306) and port_open(6379))
    if fallback:
        print("setup: brew dependencies unavailable; using isolated project runtime")
        runtime_start()
    # 只尝试一次当前管理员认证；失败立即停止，不重置或猜测密码。
    query = ("SELECT COUNT(*) FROM information_schema.SCHEMATA WHERE SCHEMA_NAME='texas_holdem';"
             "SELECT COUNT(*) FROM mysql.user WHERE User='texas_holdem_app';")
    p = mysql(query, admin=True, runtime=fallback)
    require(p.returncode == 0, "ADMIN_AUTH_OR_PROBE_FAILED")
    require(p.stdout.split() == ["0", "0"], "DEDICATED_DATABASE_OR_USER_EXISTS_STOP")
    redis_port = 6380 if fallback else 6379
    mysql_port = 3307 if fallback else 3306
    redis_db = 15 if fallback else 0
    redis_ping({"redis_host": "127.0.0.1", "redis_port": redis_port,
                "redis_db": redis_db, "redis_password": ""})
    password = secrets.token_urlsafe(36)
    dsn = ("texas_holdem_app:" + password + "@tcp(127.0.0.1:" + str(mysql_port) +
           ")/texas_holdem?parseTime=true")
    # 先持久化直连凭据（O_EXCL）以便 DDL 部分失败时保留恢复资料，再建库建用户。
    _write_local_config(local_config, dsn, "127.0.0.1:" + str(redis_port), redis_db, "")
    _ensure_env_file()
    sql = ("CREATE DATABASE texas_holdem CHARACTER SET utf8mb4;"
           "CREATE USER 'texas_holdem_app'@'localhost' IDENTIFIED BY '" + password + "';"
           "GRANT SELECT,INSERT,UPDATE,CREATE,REFERENCES ON texas_holdem.* "
           "TO 'texas_holdem_app'@'localhost';")
    require(mysql(sql, admin=True, runtime=fallback).returncode == 0,
            "INIT_FAILED_CONFIG_RETAINED_NO_ROLLBACK")
    print("init: dedicated database/user created; scoped grants; no root changes")
    doctor()


def sanitize_output(text, values=None):
    """保留失败上下文，仅脱敏凭据/DSN；不读取用户模型配置。"""
    confidential = dict(os.environ)
    confidential.update(values or {})
    for key, value in confidential.items():
        if value and (key == "POKER_MYSQL_DSN" or re.search(r"PASSWORD|TOKEN|SECRET|API_?KEY|MYSQL_PWD", key, re.I)):
            text = text.replace(value, "[凭据已脱敏]")
            if key == "POKER_MYSQL_DSN" and (match := DSN.fullmatch(value)):
                text = text.replace(match[1], "[凭据已脱敏]")
    text = re.sub(r"[^\s\"']+@(?:tcp|unix)\([^\r\n]*", "[DSN已脱敏]", text)
    text = re.sub(r"(?i)(\bauthorization[ \t]*[:=][ \t]*)([^\r\n]+)", r"\1[凭据已脱敏]", text)
    text = re.sub(r'(?i)(\b[\"\']?(?:password|api_?key|access_token|secret)[\"\']?[ \t]*[:=][ \t]*)([\"\'][^\"\'\r\n]*[\"\']|[^\s,}\r\n]+)',
                  r"\1[凭据已脱敏]", text)
    return text


def safe_output(text, values=None):
    print(sanitize_output(text, values), end="" if text.endswith("\n") else "\n")


def action_env(action):
    # 先验证受保护文件仍为 paid=false；仅显式 dev-live action 改变子进程开关。
    # 非敏感旧值不注入，避免盖过 YAML；白名单 env 与 dev-live 为显式覆盖。
    values = dict(load_env())
    return service_env(values, action=action)


def frontend():
    require((ROOT / "web/package.json").is_file(), "FRONTEND_PACKAGE_MISSING")
    lock = ROOT / "web/package-lock.json"
    require(lock.is_file(), "FRONTEND_LOCK_MISSING")
    for stage, args in (("npm-ci", ["npm", "--prefix", "web", "ci"]),
                        ("frontend-build", ["npm", "--prefix", "web", "run", "build"])):
        p = captured(args, timeout=300)
        safe_output(p.stdout + p.stderr)
        print("stage=" + stage + "; exit=" + str(p.returncode) + "; output=credential-redacted-only")
        require(p.returncode == 0, "FRONTEND_STAGE_FAILED_" + stage.upper().replace("-", "_"))


def go_task(action):
    require((ROOT / "web/dist/index.html").is_file(), "FRONTEND_DIST_MISSING")
    if action in {"dev", "dev-live"}:
        # 启动服务前清理 8081 上残留的本项目 server，确保新进程嵌入最新 dist。
        stop_stale_server()
    env = action_env(action) if action in {"dev", "dev-live", "migrate"} else child_env()
    if action == "build":
        (ROOT / "tmp").mkdir(exist_ok=True)
        args = ["go", "build", "-o", "tmp/texas-holdem", "./cmd/server"]
    elif action == "test":
        args = ["go", "test", "-count=1", "-v", "./..."]
    else:
        args = ["go", "run", "./cmd/server"] + (["migrate"] if action == "migrate" else [])
    # 长期 dev 输出同样逐行在本进程过滤，避免写原始日志。
    if action in {"dev", "dev-live"}:
        with subprocess.Popen(args, cwd=ROOT, env=env, stdout=subprocess.PIPE,
                              stderr=subprocess.STDOUT, text=True, start_new_session=True) as p:
            try:
                assert p.stdout is not None
                for line in p.stdout:
                    safe_output(line, env)
            except KeyboardInterrupt:
                # go run 的子二进制与编译器都属于本轮进程组，不能留下监听进程。
                os.killpg(p.pid, signal.SIGTERM)
            try:
                return p.wait(timeout=20)
            except subprocess.TimeoutExpired:
                os.killpg(p.pid, signal.SIGKILL)
                p.wait()
                raise SafeError("DEV_STOP_TIMEOUT")
    p = captured(args, env=env, timeout=300)
    safe_output(p.stdout + p.stderr, env)
    print(action + ": " + ("passed" if p.returncode == 0 else "failed") +
          "; exit=" + str(p.returncode))
    return p.returncode


def run():
    """一键入口：先编译前端（npm ci + build 生成最新 web/dist），再前台启动服务。

    复用 frontend() 与 dev 的启动逻辑（go_task("dev")）：env 仍经 action_env 走
    service_env 的绝对 POKER_CONFIG_FILE 注入与凭据加载，前端失败即中止、不启动服务。
    前端构建后、启动服务前清理 8081 残留 server（go_task 内亦会调用，幂等）。
    """
    frontend()
    stop_stale_server()
    return go_task("dev")


def main():
    require(len(sys.argv) == 2, "USAGE_LOCAL_ACTION_REQUIRED")
    action = sys.argv[1]
    if action == "setup":
        setup()
    elif action == "doctor":
        doctor()
    elif action == "frontend":
        frontend()
    elif action == "run":
        return run()
    elif action == "diagnose":
        diagnose()
    elif action in {"start", "stop", "status"}:
        {"start": runtime_start, "stop": runtime_stop, "status": runtime_status}[action]()
    elif action == "probe-dev":
        probe_dev()
    elif action in {"verify-config-refactor", "verify-final", "verify-port8081"}:
        return {"verify-config-refactor": verify_config_refactor, "verify-final": verify_final,
                "verify-port8081": verify_port8081}[action]()
    elif action in {"dev", "dev-live", "migrate", "test", "build"}:
        return go_task(action)
    else:
        raise SafeError("UNKNOWN_ACTION")
    return 0


def _yaml_http_port(text):
    match = re.search(r"(?m)^\s*port:\s*(\d+)\s*$", text)
    if match is None:
        raise SafeError("CONFIG_PORT_NOT_FOUND")
    return int(match.group(1))


def _stage(log, label, argv, *, env, values, timeout=2400, check=None):
    """执行一个阶段：完整安全输出落证据文档，不截断失败信息。"""
    print("config-verification: running " + label, flush=True)
    result = captured(argv, env=env, timeout=timeout)
    output = sanitize_output(result.stdout + result.stderr, values)
    verdict = result.returncode == 0 and (check(output) if check else True)
    log.write("\n### " + label + "\n\n```text\n" + output +
              ("" if output.endswith("\n") else "\n") + "```\n\n退出码：" + str(result.returncode) +
              "；判定：" + ("PASS" if verdict else "FAIL") + "\n")
    log.flush()
    print("config-verification: " + label + "; exit=" + str(result.returncode) +
          "; verdict=" + ("PASS" if verdict else "FAIL"), flush=True)
    return verdict


def _go_contract_check(output):
    # 防止 -run 无匹配时 go test 以 0 退出却"假通过"。
    return "--- PASS:" in output and "[no tests to run]" not in output


def verify_config_refactor():
    """默认 8081 与临时 8083 的真实链路验证；只改临时 YAML 端口，不碰主 YAML/local。

    证据完整写入 docs/CONFIG_REFACTOR_VERIFICATION.md；历史 raw 文档不改。
    """
    values = load_env(e2e=True)  # 仅本地脱敏使用；不修改受保护文件。
    default_config = ROOT / "config/config.yaml"
    require(default_config.is_file(), "CONFIG_FILE_MISSING")
    require(_yaml_http_port(default_config.read_text()) == 8081, "DEFAULT_CONFIG_PORT_UNEXPECTED")
    (ROOT / "tmp").mkdir(exist_ok=True)
    failures = 0
    with tempfile.TemporaryDirectory(prefix="config-refactor-", dir=ROOT / "tmp") as temp:
        temp_config = Path(temp) / "config.yaml"
        text = default_config.read_text()
        replaced = text.replace("port: 8081", "port: 8083", 1)
        require("port: 8083" in replaced and "port: 8081" not in replaced, "TEMP_CONFIG_REWRITE_FAILED")
        temp_config.write_text(replaced)   # 无秘密；内容不打印。
        temp_config.chmod(0o600)
        default_env = service_env(values, e2e=True)
        temp_env = service_env(values, e2e=True, explicit={"POKER_CONFIG_FILE": str(temp_config)})
        saved = os.environ.pop("POKER_CONFIG_FILE", None)
        try:
            with CONFIG_EVIDENCE.open("a", encoding="utf-8") as log:
                # 0) 脚本单测 / typecheck / build
                failures += not _stage(log, "make test-scripts", ["make", "test-scripts"],
                                       env=default_env, values=values, timeout=600)
                failures += not _stage(log, "web typecheck", ["npm", "--prefix", "web", "run", "typecheck"],
                                       env=default_env, values=values, timeout=600)
                if not _stage(log, "make build", ["make", "build"], env=default_env, values=values, timeout=3000):
                    failures += 1
                    log.write("\n构建失败：依赖阶段未执行。\n")
                    log.flush()
                    return 1

                # 1) 默认 8081 契约 + 全量真实链路
                require(not port_open(8081), "PORT_8081_IN_USE_STOP_NO_KILL")
                config = config_public(env=config_cli_env())
                contract_ok = config["http_port"] == 8081 and set(config) == CONFIG_PUBLIC_KEYS
                log.write("\n默认 CLI：字段=" + ",".join(sorted(config)) + "；http_port=" +
                          str(config["http_port"]) + "；strict5=" + str(set(config) == CONFIG_PUBLIC_KEYS) + "\n")
                log.flush()
                failures += not contract_ok
                failures += not _stage(log, "default 8081: make migrate", ["make", "migrate"],
                                       env=default_env, values=values)
                failures += not _stage(log, "default 8081: make e2e", ["make", "e2e"],
                                       env=default_env, values=values)
                failures += not _stage(log, "default 8081: make e2e-browser", ["make", "e2e-browser"],
                                       env=default_env, values=values)
                failures += not _stage(log, "default 8081: go contract tests",
                                       ["go", "test", "-mod=readonly", "-count=1", "-v",
                                        "./internal/conf", "./internal/server", "-run",
                                        "TestYAMLConfigWiringAndPublicWhitelist|TestPublicDoesNotRequireStorageSecret|"
                                        "TestPort8081SecurityContract|TestDynamicListenerAndOriginWhitelist"],
                                       env=default_env, values=values, timeout=600, check=_go_contract_check)
                # Go 全量 race 与 vet：与业务测试一致使用干净环境（不注入配置）。
                failures += not _stage(log, "go test -mod=readonly -race -count=1 ./...",
                                       ["go", "test", "-mod=readonly", "-race", "-count=1", "./..."],
                                       env=child_env(), values=values, timeout=3000)
                failures += not _stage(log, "go vet ./...", ["go", "vet", "./..."],
                                       env=child_env(), values=values, timeout=600)

                # 2) 仅改临时 YAML 端口为 8083
                require(not port_open(8083), "PORT_8083_IN_USE_STOP_NO_KILL")
                temp_public = config_public(env={**config_cli_env(), "POKER_CONFIG_FILE": str(temp_config)})
                temp_ok = temp_public["http_port"] == 8083 and set(temp_public) == CONFIG_PUBLIC_KEYS
                log.write("\n临时 8083 CLI：字段=" + ",".join(sorted(temp_public)) + "；http_port=" +
                          str(temp_public["http_port"]) + "；strict5=" + str(set(temp_public) == CONFIG_PUBLIC_KEYS) + "\n")
                log.flush()
                failures += not temp_ok
                failures += not _stage(log, "temp 8083: e2e all", ["python3", "tests/e2e/run.py", "all"],
                                       env=temp_env, values=values)
                failures += not _stage(log, "temp 8083: browser", ["python3", "tests/e2e/browser/run_browser.py"],
                                       env=temp_env, values=values)
                failures += not _stage(log, "temp 8083: vite proxy -> Go", ["python3", "tests/e2e/vite_proxy.py"],
                                       env=temp_env, values=values)

                # 3) 主 YAML 未被改动；自身 HTTP 已释放
                unchanged = _yaml_http_port(default_config.read_text()) == 8081
                log.write("\n主 YAML 端口仍为 8081：" + str(unchanged) + "\n")
                failures += not unchanged
                released = not (port_open(8081) or port_open(8083))
                log.write("自身 HTTP 8081/8083 已释放：" + str(released) + "\n")
                failures += not released
                log.write("\n失败阶段数：" + str(failures) + "\n")
                log.flush()
        finally:
            if saved is not None:
                os.environ["POKER_CONFIG_FILE"] = saved
        print("config-verification: failures=" + str(failures), flush=True)
    return 1 if failures else 0


def verify_port8081():
    # 历史命令别名保留；行为已改为按 YAML/CLI 派生的动态验证。
    return verify_config_refactor()


def verify_final():
    # 历史命令别名保留；行为已改为按 YAML/CLI 派生的动态验证。
    return verify_config_refactor()


def probe_dev():
    config = config_public()
    port, base = config["http_port"], config["http_base_url"]
    require(not port_open(port), "DEV_PROBE_HTTP_PORT_IN_USE_STOP_NO_KILL")
    binary = ROOT / "tmp/texas-holdem"
    require(binary.is_file(), "DEV_PROBE_BUILD_REQUIRED")
    values = load_env()
    conn = storage_config()
    existing = mysql_query(conn, "SELECT COUNT(*) FROM games WHERE active_slot=1;")
    require(existing.returncode == 0, "DEV_PROBE_MIGRATE_REQUIRED")
    require(existing.stdout.strip() == "0", "DEV_PROBE_EXISTING_ACTIVE_GAME_STOP")
    p = subprocess.Popen([str(binary)], cwd=ROOT, env=service_env(values, action="dev"),
                         stdout=subprocess.DEVNULL, stderr=subprocess.PIPE)
    errors = set()

    def consume():
        assert p.stderr is not None
        for raw in p.stderr:
            text = raw.decode("utf-8", errors="replace").strip()
            if re.fullmatch(r"MODEL_[A-Z_]+|CONFIG_[A-Z_]+|SCHEMA_INVALID|STORAGE_UNAVAILABLE|WEB_ASSETS_UNAVAILABLE|INSTANCE_LOCK_UNAVAILABLE", text):
                errors.add(text)
            # 未知日志丢弃；不保存、不转发原文。

    thread = threading.Thread(target=consume, daemon=True)
    thread.start()
    http = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    try:
        deadline = time.monotonic() + 20
        health = None
        while time.monotonic() < deadline:
            if p.poll() is not None:
                thread.join(timeout=2)
                raise SafeError("DEV_PROBE_START_FAILED:" + (",".join(sorted(errors)) or "UNKNOWN_STAGE_ERROR"))
            try:
                with http.open(base + "/api/v1/health", timeout=3) as response:
                    health = json.load(response)
                break
            except (OSError, urllib.error.URLError):
                time.sleep(0.1)
        require(health and not health["allow_paid_models"], "DEV_PROBE_HEALTH_OR_PAID_FLAG_INVALID")
        with http.open(base + "/api/v1/models", timeout=3) as response:
            models = json.load(response)["models"]
        count = sum(m["ref"] != "mock" for m in models)
        print("dev-probe: HTTP health=200; paid-disabled; model-count=" + str(len(models)) +
              "; real-model-count=" + str(count) + "; provider-calls=0")
        print("dev-probe: catalog=" + ("explicit-env" if os.environ.get("POKER_CATALOG_PATH")
                                       else "yaml-auto"))
        print("dev-probe: tests=1 passed=1 failed=0")
    finally:
        if p.poll() is None:
            p.terminate()
            try:
                p.wait(timeout=20)
            except subprocess.TimeoutExpired:
                p.kill()
                p.wait()
                raise SafeError("DEV_PROBE_STOP_TIMEOUT")
        thread.join(timeout=2)
        print("dev-probe: own HTTP process stopped; dependencies retained")


def port_open(port):
    try:
        with socket.create_connection(("127.0.0.1", port), timeout=1):
            return True
    except OSError:
        return False


def private_directory(path):
    if not path.exists():
        path.mkdir(mode=0o700)
    s = path.lstat()
    require(stat.S_ISDIR(s.st_mode) and not path.is_symlink() and
            stat.S_IMODE(s.st_mode) == 0o700 and s.st_uid == os.getuid(),
            "RUNTIME_DIRECTORY_PERMISSION_INVALID")


def process_arguments(pid):
    """Darwin KERN_PROCARGS2：保留 argv 的 NUL 边界，不从 ps 文本猜空格。"""
    require(sys.platform == "darwin", "RUNTIME_ARGV_PLATFORM_UNSUPPORTED")
    libc = ctypes.CDLL(None, use_errno=True)
    mib = (ctypes.c_int * 3)(1, 49, pid)  # CTL_KERN, KERN_PROCARGS2
    size = ctypes.c_size_t()
    require(libc.sysctl(mib, 3, None, ctypes.byref(size), None, 0) == 0 and
            4 < size.value <= 4 << 20, "RUNTIME_ARGV_INSPECTION_FAILED")
    buffer = ctypes.create_string_buffer(size.value)
    require(libc.sysctl(mib, 3, buffer, ctypes.byref(size), None, 0) == 0,
            "RUNTIME_ARGV_INSPECTION_FAILED")
    raw = buffer.raw[:size.value]
    argc = int.from_bytes(raw[:4], sys.byteorder, signed=True)
    require(0 < argc <= 512, "RUNTIME_ARGV_INVALID")
    end = raw.find(b"\0", 4)
    require(end > 4, "RUNTIME_ARGV_INVALID")
    executable = os.fsdecode(raw[4:end])
    offset = end + 1
    while offset < len(raw) and raw[offset] == 0:
        offset += 1
    argv = []
    for _ in range(argc):
        end = raw.find(b"\0", offset)
        require(end >= offset, "RUNTIME_ARGV_INVALID")
        argv.append(os.fsdecode(raw[offset:end]))
        offset = end + 1
    # 环境区不解析、不返回；原始 buffer 不输出。
    return executable, argv


def expected_binary(name):
    if name == "mysql":
        return Path(mysql_binary()).with_name("mysqld").resolve(strict=True)
    require(name == "redis", "RUNTIME_SERVICE_UNKNOWN")
    prefix = captured(["brew", "--prefix", "redis"])
    require(prefix.returncode == 0, "REDIS_BINARY_UNAVAILABLE")
    return (Path(prefix.stdout.strip()) / "bin/redis-server").resolve(strict=True)


def process_snapshot(pid):
    result = captured(["ps", "-p", str(pid), "-o", "uid=", "-o", "stat=", "-o", "lstart="])
    if result.returncode != 0 or not result.stdout.strip():
        return None
    fields = result.stdout.strip().split(None, 2)
    require(len(fields) == 3 and fields[0] == str(os.getuid()), "RUNTIME_PID_OWNER_MISMATCH_STOP")
    require(re.fullmatch(r"[SRITUVWZX][+<NLslEX]*", fields[1]), "RUNTIME_PROCESS_STATUS_INVALID")
    if fields[1].startswith(("Z", "X")):
        return None
    # 只返回 uid + 启动时间，绝不收集 ps command 或用户环境。
    return fields[0], fields[2]


def canonical_path(value):
    path = Path(value)
    require(path.is_absolute(), "RUNTIME_IDENTITY_PATH_NOT_ABSOLUTE")
    return path.resolve(strict=True)


def exact_option(argv, option):
    values = []
    for index, value in enumerate(argv[1:], 1):
        if value == option:
            require(index + 1 < len(argv), "RUNTIME_ARGV_OPTION_INVALID")
            values.append(argv[index + 1])
        elif value.startswith(option + "="):
            values.append(value[len(option) + 1:])
    require(len(values) == 1, "RUNTIME_ARGV_OPTION_MISSING_OR_DUPLICATE")
    return values[0]


# --- 应用 server（8081）残留清理：只清理本项目 go run 启动的 server，绝不误杀 ---

STALE_SERVER_PORT = 8081


def go_cache():
    """go run 编译出的临时可执行文件所在缓存目录（GOCACHE），用于核验 server 身份。"""
    result = captured(["go", "env", "GOCACHE"])
    require(result.returncode == 0 and result.stdout.strip(), "GO_CACHE_UNAVAILABLE")
    path = Path(result.stdout.strip())
    require(path.is_absolute(), "GO_CACHE_NOT_ABSOLUTE")
    return path.resolve()


def listener_pids(port):
    """返回监听 port 的 PID 列表；无监听返回空列表。"""
    result = captured(["lsof", "-nP", "-iTCP:" + str(port), "-sTCP:LISTEN", "-t"])
    pids = []
    for line in (result.stdout or "").splitlines():
        line = line.strip()
        if not line:
            continue
        require(re.fullmatch(r"[0-9]+", line), "STALE_SERVER_LSOF_OUTPUT_INVALID")
        pids.append(int(line))
    return sorted(set(pids))


def project_process_cwd(pid):
    """核验进程 cwd 为本项目根目录；不是即抛安全错误。"""
    result = captured(["lsof", "-nP", "-a", "-p", str(pid), "-d", "cwd", "-F", "n"])
    require(result.returncode == 0, "STALE_SERVER_CWD_INSPECTION_FAILED")
    require("n" + str(ROOT.resolve()) in result.stdout.splitlines(), "STALE_SERVER_CWD_MISMATCH")


def server_listener_identity(pid):
    """核验 pid 是本项目 go run 编译出的 server 监听进程；任何不匹配都抛 MANUAL 且不杀。

    身份依据（全部来自实时采集，不信任 ps 文本）：进程存在且归属当前用户、原生
    argv 可读、可执行文件在 GOCACHE 下（go run 产物）、cwd 为本项目根目录、argv 含
    cmd/server/texas-holdem（编译产物 argv 可仅一个可执行路径）。采集前后启动时间一致。
    """
    require(sys.platform == "darwin", "STALE_SERVER_PLATFORM_UNSUPPORTED")
    before = process_snapshot(pid)
    require(before is not None, "STALE_SERVER_UNKNOWN_PROCESS_MANUAL")
    assert before is not None
    try:
        executable, argv = process_arguments(pid)
        exe = canonical_path(executable)
        cache = go_cache()
        project_process_cwd(pid)
        argv_ok = (len(argv) <= 1 or
                   any(token in " ".join(argv) for token in ("cmd/server", "texas-holdem")))
    except (SafeError, OSError):
        raise SafeError("STALE_SERVER_UNKNOWN_PROCESS_MANUAL")
    require(cache == exe or cache in exe.parents or "go-build" in exe.parts,
            "STALE_SERVER_UNKNOWN_PROCESS_MANUAL")
    require(argv_ok, "STALE_SERVER_UNKNOWN_PROCESS_MANUAL")
    after = process_snapshot(pid)
    require(before == after, "STALE_SERVER_PID_CHANGED_DURING_INSPECTION")
    return {"pid": pid, "started": list(before)}


def go_run_parent_identity(pid):
    """若 pid 的父进程为本项目 go run，返回其身份；无法确认一律返回 None（绝不误杀）。"""
    if sys.platform != "darwin":
        return None
    try:
        result = captured(["ps", "-o", "ppid=", "-p", str(pid)])
        if result.returncode != 0:
            return None
        text = (result.stdout or "").strip()
        if re.fullmatch(r"[0-9]+", text) is None:
            return None
        ppid = int(text)
        if ppid <= 1:
            return None
        before = process_snapshot(ppid)
        if before is None:
            return None
        executable, argv = process_arguments(ppid)
        exe = canonical_path(executable)
        joined = " ".join(argv)
        require(exe.name == "go" and argv[:2] == ["go", "run"] and
                ("cmd/server" in joined or "texas-holdem" in joined),
                "STALE_SERVER_PARENT_NOT_GO_RUN")
        project_process_cwd(ppid)
        after = process_snapshot(ppid)
        require(before == after, "STALE_SERVER_PARENT_CHANGED_DURING_INSPECTION")
        return {"pid": ppid, "started": list(before)}
    except (SafeError, OSError):
        return None


def _stop_pid(identity):
    """核验身份未变后 SIGTERM→等待→超时 SIGKILL；PID 复用即中止。"""
    pid = identity["pid"]
    current = process_snapshot(pid)
    if current is None:
        return
    require(list(current) == identity["started"], "STALE_SERVER_PID_REUSED_BEFORE_SIGNAL")
    os.kill(pid, signal.SIGTERM)
    deadline = time.monotonic() + 20
    while time.monotonic() < deadline:
        remaining = process_snapshot(pid)
        if remaining is None:
            return
        require(list(remaining) == identity["started"], "STALE_SERVER_PID_REUSED_AFTER_SIGNAL")
        time.sleep(0.1)
    remaining = process_snapshot(pid)
    require(remaining is not None and list(remaining) == identity["started"],
            "STALE_SERVER_PID_REUSED_BEFORE_KILL")
    os.kill(pid, signal.SIGKILL)
    deadline = time.monotonic() + 10
    while time.monotonic() < deadline and process_snapshot(pid) is not None:
        time.sleep(0.1)
    require(process_snapshot(pid) is None, "STALE_SERVER_STOP_TIMEOUT")


def stop_stale_server(port=STALE_SERVER_PORT):
    """启动服务前清理 port 上本项目残留 server：空闲跳过，陌生进程绝不误杀。

    先 SIGTERM 监听子进程，再兜底清理其 go run 父进程。顺序不可颠倒：先杀父进程会让
    子进程被 reparent 后继续占用端口。仅处理监听该端口的本项目 server，不触碰 runtime
    隔离的 mysql/redis（那由 start/stop 管理）。
    """
    if not port_open(port):
        print("stale-server: port " + str(port) + " free; nothing to stop")
        return False
    pids = listener_pids(port)
    require(len(pids) == 1, "STALE_SERVER_UNKNOWN_PROCESS_MANUAL")
    identity = server_listener_identity(pids[0])
    parent = go_run_parent_identity(pids[0])
    print("stale-server: stopping project server pid=" + str(identity["pid"]))
    _stop_pid(identity)
    if parent is not None:
        _stop_pid(parent)
    print("stale-server: port " + str(port) + " cleared")
    return True


def process_state(name):
    path = RUNTIME / (name + "-process.json")
    if not path.exists():
        return None
    state = json.loads(protected_read(path))
    pid = state.get("pid")
    require(type(pid) is int and pid > 1, "RUNTIME_PID_INVALID")
    before = process_snapshot(pid)
    if before is None:
        return None
    require("started" not in state or state["started"] == list(before), "RUNTIME_PID_REUSED_STOP")
    expected = expected_binary(name)
    kernel_executable, argv = process_arguments(pid)
    require(canonical_path(kernel_executable) == expected, "RUNTIME_KERNEL_EXECUTABLE_MISMATCH_STOP")
    executable = captured(["lsof", "-a", "-p", str(pid), "-d", "txt", "-F", "n"])
    cwd = captured(["lsof", "-a", "-p", str(pid), "-d", "cwd", "-F", "n"])
    require(executable.returncode == 0 and cwd.returncode == 0, "RUNTIME_PROCESS_PATH_INSPECTION_FAILED")
    require("n" + str(expected) in executable.stdout.splitlines(), "RUNTIME_EXECUTABLE_MISMATCH_STOP")
    runtime = RUNTIME.resolve(strict=True)
    if name == "mysql":
        require(canonical_path(exact_option(argv, "--datadir")) == runtime / "mysql-data",
                "RUNTIME_DATADIR_MISMATCH_STOP")
        require(canonical_path(exact_option(argv, "--socket")) == runtime / "mysql.sock",
                "RUNTIME_SOCKET_MISMATCH_STOP")
        require(canonical_path(exact_option(argv, "--pid-file")) == runtime / "mysql.pid",
                "RUNTIME_PID_FILE_MISMATCH_STOP")
        require(exact_option(argv, "--port") == "3307" and
                exact_option(argv, "--bind-address") == "127.0.0.1", "RUNTIME_ENDPOINT_MISMATCH_STOP")
        # mysqld 启动后会 chdir 到 datadir，不沿用 Popen 的初始 cwd。
        require("n" + str(runtime / "mysql-data") in cwd.stdout.splitlines(), "RUNTIME_CWD_MISMATCH_STOP")
    else:
        require("n" + str(runtime / "redis-data") in cwd.stdout.splitlines(), "RUNTIME_CWD_MISMATCH_STOP")
        if "--dir" in argv or any(arg.startswith("--dir=") for arg in argv):
            # 本项目从不传 Redis config 文件；必须是完整固定参数列表，不能接受相似 config 路径。
            expected_args = ["--bind", "127.0.0.1", "--port", "6380", "--protected-mode", "yes",
                             "--dir", str(runtime / "redis-data"), "--save", "", "--appendonly", "no",
                             "--logfile", str(runtime / "redis-error.log")]
            require(argv[1:] == expected_args, "RUNTIME_REDIS_ARGV_MISMATCH_STOP")
            require(canonical_path(exact_option(argv, "--dir")) == runtime / "redis-data",
                    "RUNTIME_DATADIR_MISMATCH_STOP")
            require(exact_option(argv, "--port") == "6380" and
                    exact_option(argv, "--bind") == "127.0.0.1", "RUNTIME_ENDPOINT_MISMATCH_STOP")
        else:
            # Redis 可改写 argv；cwd + 真 executable + 该 PID 的精确 listener 必须均匹配。
            nonempty = [arg.strip() for arg in argv if arg.strip()]
            require(len(nonempty) == 1, "RUNTIME_REDIS_REWRITTEN_ARGV_UNRECOGNIZED_STOP")
            title = nonempty[0]
            if title != "redis-server 127.0.0.1:6380":
                suffix = " 127.0.0.1:6380"
                require(title.endswith(suffix), "RUNTIME_REDIS_REWRITTEN_ARGV_UNRECOGNIZED_STOP")
                require(canonical_path(title[:-len(suffix)]) == expected,
                        "RUNTIME_REDIS_REWRITTEN_EXECUTABLE_MISMATCH_STOP")
            listener = captured(["lsof", "-nP", "-a", "-p", str(pid), "-iTCP", "-sTCP:LISTEN", "-F", "n"])
            require(listener.returncode == 0 and
                    [line for line in listener.stdout.splitlines() if line.startswith("n")] == ["n127.0.0.1:6380"],
                    "RUNTIME_REDIS_LISTENER_MISMATCH_STOP")
    after = process_snapshot(pid)
    require(before == after, "RUNTIME_PID_CHANGED_DURING_INSPECTION_STOP")
    # 身份由固定路径/实时采集生成；不信任 meta.binary/meta.signature。
    return {"pid": pid, "started": list(before), "identity": (str(expected), tuple(argv), before)}


def save_state(name, state):
    path = RUNTIME / (name + "-process.json")
    temporary = RUNTIME / (name + "-process.json.tmp")
    fd = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, "w") as f:
        json.dump(state, f)
    os.replace(temporary, path)


def runtime_start():
    private_directory(RUNTIME)
    data = RUNTIME / "mysql-data"
    cache = RUNTIME / "redis-data"
    private_directory(data)
    private_directory(cache)
    binary = str(Path(mysql_binary()).with_name("mysqld"))
    prefix = captured(["brew", "--prefix", "redis"])
    require(prefix.returncode == 0, "REDIS_BINARY_UNAVAILABLE")
    redis = str(Path(prefix.stdout.strip()) / "bin/redis-server")
    common = [binary, "--no-defaults", "--datadir=" + str(data),
              "--log-error=" + str(RUNTIME / "mysql-error.log")]
    marker = RUNTIME / "mysql-initialized"
    if not marker.exists():
        require(not any(data.iterdir()), "RUNTIME_EXISTING_DATA_NOT_INITIALIZED_STOP")
        p = captured(common + ["--initialize-insecure"], timeout=120)
        require(p.returncode == 0, "RUNTIME_MYSQL_INITIALIZE_FAILED_SEE_DIAGNOSE")
        fd = os.open(marker, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
        os.close(fd)
        print("runtime: isolated mysql initialized; existing brew data untouched")
    configs = {
        "mysql": (common + ["--bind-address=127.0.0.1", "--port=3307", "--mysqlx=OFF",
                            "--socket=" + str(RUNTIME / "mysql.sock"),
                            "--pid-file=" + str(RUNTIME / "mysql.pid"), "--skip-log-bin",
                            "--general-log=OFF", "--slow-query-log=OFF"], 3307, "--datadir=" + str(data)),
        "redis": ([redis, "--bind", "127.0.0.1", "--port", "6380", "--protected-mode", "yes",
                   "--dir", str(cache), "--save", "", "--appendonly", "no",
                   "--logfile", str(RUNTIME / "redis-error.log")], 6380, str(cache))}
    for name, (args, port, signature) in configs.items():
        existing = process_state(name)
        if existing:
            require(port_open(port), "RUNTIME_PROCESS_PORT_NOT_READY")
            print("runtime: " + name + " already-running; identity verified")
            continue
        require(not port_open(port), "RUNTIME_PORT_IN_USE_STOP_NO_KILL")
        p = subprocess.Popen(args, cwd=RUNTIME, env=child_env(), start_new_session=True,
                             stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        save_state(name, {"pid": p.pid, "binary": args[0], "signature": signature})
        deadline = time.monotonic() + 30
        while time.monotonic() < deadline and p.poll() is None and not port_open(port):
            time.sleep(0.2)
        require(p.poll() is None and port_open(port), "RUNTIME_" + name.upper() + "_START_FAILED_SEE_DIAGNOSE")
        verified = process_state(name)
        require(verified is not None, "RUNTIME_NEW_PROCESS_IDENTITY_UNAVAILABLE")
        assert verified is not None
        save_state(name, {"pid": p.pid, "started": verified["started"]})
        print("runtime: " + name + " running; loopback-port=" + str(port))


def runtime_stop():
    require(RUNTIME.is_dir(), "RUNTIME_NOT_PRESENT")
    private_directory(RUNTIME)
    for name in ("redis", "mysql"):
        state = process_state(name)
        if state:
            current = process_state(name)
            require(current is not None and current == state, "RUNTIME_PID_CHANGED_BEFORE_SIGNAL_STOP")
            assert current is not None
            os.kill(current["pid"], signal.SIGTERM)
            deadline = time.monotonic() + 30
            while time.monotonic() < deadline:
                remaining = process_snapshot(current["pid"])
                if remaining is None:
                    break
                require(list(remaining) == current["started"], "RUNTIME_PID_REUSED_AFTER_SIGNAL_STOP")
                time.sleep(0.2)
            require(process_snapshot(current["pid"]) is None, "RUNTIME_STOP_TIMEOUT_NO_FORCE_KILL")
            print("runtime: " + name + " stopped; data retained")
        else:
            print("runtime: " + name + " not-running")


def runtime_status():
    for name, port in (("mysql", 3307), ("redis", 6380)):
        state = process_state(name)
        print("runtime: " + name + "; identity=" + ("verified" if state else "not-running") +
              "; port=" + str(port) + "; open=" + str(port_open(port)))


def diagnose():
    for label, port in (("mysql", 3306), ("redis", 6379)):
        try:
            with socket.create_connection(("127.0.0.1", port), timeout=2):
                print(label + ": loopback-port-open")
        except OSError:
            print(label + ": loopback-port-closed")
    prefix = captured(["brew", "--prefix"]).stdout.strip()
    base = Path(prefix) / "var"
    for path in (Path(prefix), base, base / "mysql", base / "log", Path(prefix) / "etc/redis.conf"):
        if path.exists():
            mode = path.stat()
            print("path=" + str(path) + "; mode=" + oct(stat.S_IMODE(mode.st_mode)) +
                  "; owner-current=" + str(mode.st_uid == os.getuid()) +
                  "; readable=" + str(os.access(path, os.R_OK)) +
                  "; writable=" + str(os.access(path, os.W_OK)))
    for name in ("mysql@8.4", "redis"):
        plist = Path.home() / "Library/LaunchAgents" / ("sh.brew." + name + ".plist")
        if plist.is_file():
            config = plistlib.loads(plist.read_bytes())
            args = config.get("ProgramArguments", [])
            if args:
                print(name + ": launch-executable-exists=" + str(Path(args[0]).is_file()))
            for key in ("WorkingDirectory", "StandardErrorPath", "StandardOutPath"):
                if key in config:
                    path = Path(config[key])
                    print(name + ": " + key + "-parent-exists=" + str(path.parent.is_dir()))
    print("mysql: data-directory-exists=" + str((base / "mysql").is_dir()))
    files = list((base / "mysql").glob("*.err")) + [base / "log/redis.log",
            RUNTIME / "mysql-error.log", RUNTIME / "redis-error.log"]
    for path in files:
        if not path.is_file():
            continue
        # 日志只在本进程检查；仅输出固定分类，不输出任何原文/配置。
        text = path.read_bytes()[-65536:].decode("utf-8", errors="replace").lower()
        print("log-path=" + str(path))
        if path == base / "log/redis.log":
            words = {"module", "permission", "rdb", "aof", "version", "cannot", "error",
                     "failed", "abort", "config", "load", "incompatible", "dlopen", "invalid"}
            for line in text.splitlines()[-20:]:
                tokens = set(re.findall(r"[a-z]+", line)) & words
                if tokens & {"error", "failed", "abort", "cannot", "invalid"}:
                    print("redis-error-categories=" + ",".join(sorted(tokens)))
        for code in sorted(set(re.findall(r"my-\d{6}|(?:os )?errno[:= ]+\d+", text))):
            print("log-error-code=" + code.upper())
        for needle, code in (("permission denied", "PERMISSION_DENIED"),
                             ("operation not permitted", "OPERATION_NOT_PERMITTED"),
                             ("invalid mysql server downgrade", "MYSQL_DATA_VERSION_DOWNGRADE_FORBIDDEN"),
                             ("cannot downgrade", "MYSQL_DATA_VERSION_DOWNGRADE_FORBIDDEN"),
                             ("can't handle rdb format", "REDIS_RDB_VERSION_UNSUPPORTED"),
                             ("fatal error loading", "REDIS_DATA_LOAD_FAILED"),
                             ("failed to load module", "REDIS_MODULE_LOAD_FAILED"),
                             ("symbol not found", "MODULE_SYMBOL_MISSING"),
                             ("library not loaded", "MODULE_LIBRARY_MISSING"),
                             ("incompatible architecture", "MODULE_ARCH_MISMATCH"),
                             ("module", "MODULE_STAGE_SEEN"),
                             ("address already in use", "PORT_IN_USE"),
                             ("unknown variable", "UNKNOWN_VARIABLE"),
                             ("no such file", "PATH_MISSING"),
                             ("ready for connections", "MYSQL_READY_SEEN"),
                             ("ready to accept connections", "REDIS_READY_SEEN"),
                             ("can't open", "CANNOT_OPEN_FILE"),
                             ("can't open the config file", "REDIS_CONFIG_OPEN_FAILED"),
                             ("can't open the log file", "REDIS_LOG_OPEN_FAILED"),
                             ("bad directive", "REDIS_BAD_DIRECTIVE"),
                             ("aborting", "MYSQL_ABORTING"),
                             ("running as root", "MYSQL_REFUSES_ROOT"),
                             ("fatal error", "FATAL_ERROR_SEEN"),
                             ("you can find", "MYSQL_GUIDANCE_SEEN")):
            if needle in text:
                print("log-classification: " + code)


if __name__ == "__main__":
    try:
        sys.exit(main())
    except SafeError as e:
        print(str(e), file=sys.stderr)
        sys.exit(1)
    except KeyboardInterrupt:
        print("INTERRUPTED", file=sys.stderr)
        sys.exit(130)
    except OSError as e:
        print("LOCAL_OS_OPERATION_FAILED; errno=" + str(e.errno), file=sys.stderr)
        sys.exit(1)
    except json.JSONDecodeError:
        print("ENV_OR_RUNTIME_STATE_JSON_INVALID", file=sys.stderr)
        sys.exit(1)
    except Exception:
        # 不输出 traceback/异常原文，防止配置或外部程序错误携带凭据。
        print("LOCAL_OPERATION_FAILED_REDACTED", file=sys.stderr)
        sys.exit(1)
