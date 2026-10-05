// Package conf 从 YAML 与进程环境解析配置；只返回安全的配置值，
// 不记录、不格式化输出任何配置或凭据内容。
//
// 解析顺序（后者覆盖前者）：默认/指定 base YAML → 同目录 config.local.yaml → 白名单环境变量。
// base 中的显式 false / 0 与 local 的显式 false / 0 都必须保留（指针存在性合并）。
package conf

import (
	"bytes"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

const (
	// DefaultPath 是默认 base 配置文件（相对进程工作目录）。
	DefaultPath = "config/config.yaml"
	// LocalName 是同目录可选本地覆盖文件名。
	LocalName = "config.local.yaml"

	defaultHost         = "127.0.0.1"
	defaultPort         = 8081
	defaultHTTPTimeout  = 10 * time.Second
	defaultFrontendPort = 5173
	defaultRedisAddress = "127.0.0.1:6379"
	defaultRedisDB      = 0
	defaultRequestTmo   = 15 * time.Second
	defaultMaxTokens    = 256
	defaultMaxLiveCalls = 1000
	defaultHumanAction  = 60 * time.Second
	defaultAITurnDelay  = 1500 * time.Millisecond
	// 每手结束后的权威手间停留窗口：摊牌较长、弃牌较短。显式 0/负值拒绝。
	defaultHandIntervalShowdown    = 12 * time.Second
	defaultHandIntervalUncontested = 8 * time.Second
	defaultDSNEnv                  = "POKER_MYSQL_DSN"
	defaultRedisPassEnv            = "POKER_REDIS_PASSWORD"
	autoCatalog                    = "auto"
	maxFileBytes                   = 1 << 20
	maxOutputTokensLimit           = 1 << 20
	maxLiveCallsLimit              = 1000000
)

// Config 是服务侧完整配置。秘密字段只在进程内从环境变量读取，绝不写回文件或输出。
type Config struct {
	// Address 是 server.http.host:port，供监听与同源策略使用。
	Address      string
	HTTPHost     string
	HTTPPort     int
	HTTPTimeout  time.Duration
	FrontendPort int

	MySQLDSN      string
	RedisAddress  string
	RedisPassword string
	RedisDB       int

	CatalogPath     string
	AllowPaidModels bool
	MaxLiveCalls    uint32
	DecisionTimeout time.Duration
	MaxOutputTokens int

	// HumanActionTimeout 是真人被轮到行动后的等待上限；缺省 60s，显式 0/负值拒绝。
	HumanActionTimeout time.Duration

	// AITurnDelay 是 AI 每次提交动作后、推进下一个 actor 前的思考停顿；缺省 1.5s，显式 0/负值拒绝。
	AITurnDelay time.Duration

	// HandIntervalShowdown/HandIntervalUncontested 是每手结束后的权威手间停留窗口，
	// 由 gameplay.hand_interval_*_sec 配置（秒）；缺省 12s/8s，显式 0/负值拒绝。
	HandIntervalShowdown    time.Duration
	HandIntervalUncontested time.Duration

	dsnEnv      string
	passwordEnv string
	catalogRaw  string

	// 直连秘密原始值与显式存在性。dsnDirect 非空时优先于 dsnEnv；
	// passwordPresent 为 true 时以 passwordDirect 为准（空串 = 无密码，不回退 env）。
	dsnDirect       string
	passwordDirect  string
	passwordPresent bool
}

// Public 是 CLI 公共契约，只暴露监听/前端字段；禁止扩展为 Redis、catalog 或凭据。
type Public struct {
	HTTPBaseURL  string `json:"http_base_url"`
	HTTPAddress  string `json:"http_address"`
	HTTPHost     string `json:"http_host"`
	HTTPPort     int    `json:"http_port"`
	FrontendPort int    `json:"frontend_port"`
}

// Public 返回严格白名单公共视图。
func (c Config) Public() Public {
	return Public{
		HTTPBaseURL:  "http://" + c.Address,
		HTTPAddress:  c.Address,
		HTTPHost:     c.HTTPHost,
		HTTPPort:     c.HTTPPort,
		FrontendPort: c.FrontendPort,
	}
}

// ValidateStorage 校验服务运行所需的存储秘密。CLI 公共路径不调用它。
func (c Config) ValidateStorage() error {
	if c.MySQLDSN == "" || c.RedisAddress == "" {
		return errors.New("CONFIG_STORAGE_REQUIRED")
	}
	return nil
}

type fileConfig struct {
	Server struct {
		HTTP struct {
			Host    *string        `yaml:"host"`
			Port    *int           `yaml:"port"`
			Timeout *time.Duration `yaml:"timeout"`
		} `yaml:"http"`
	} `yaml:"server"`
	Frontend struct {
		DevPort *int `yaml:"dev_port"`
	} `yaml:"frontend"`
	Data struct {
		MySQL struct {
			// DSN 是完整直连 DSN（可含密码）；非空时优先于 DSNEnv 指向的环境变量。
			DSN    *string `yaml:"dsn"`
			DSNEnv *string `yaml:"dsn_env"`
		} `yaml:"mysql"`
		Redis struct {
			Addr *string `yaml:"addr"`
			DB   *int    `yaml:"db"`
			// Password 是直连密码；非空时优先于 PasswordEnv 指向的环境变量。
			// 显式空串表示「无密码」，不回退环境变量。
			Password    *string `yaml:"password"`
			PasswordEnv *string `yaml:"password_env"`
		} `yaml:"redis"`
	} `yaml:"data"`
	Agents struct {
		CatalogPath     *string        `yaml:"catalog_path"`
		RequestTimeout  *time.Duration `yaml:"request_timeout"`
		MaxOutputTokens *int           `yaml:"max_output_tokens"`
		AllowPaidModels *bool          `yaml:"allow_paid_models"`
		MaxLiveCalls    *uint32        `yaml:"max_live_calls"`
	} `yaml:"agents"`
	Gameplay struct {
		HumanActionTimeout *time.Duration `yaml:"human_action_timeout"`
		AITurnDelay        *time.Duration `yaml:"ai_turn_delay"`
		// 手间停留窗口以整秒配置；显式 null/非 int 由严格 schema 拒绝。
		HandIntervalShowdown    *int `yaml:"hand_interval_showdown_sec"`
		HandIntervalUncontested *int `yaml:"hand_interval_uncontested_sec"`
	} `yaml:"gameplay"`
}

// Load 使用默认发现路径（POKER_CONFIG_FILE 或 config/config.yaml）解析并校验存储。
func Load() (Config, error) {
	path, err := discoverPath()
	if err != nil {
		return Config{}, err
	}
	return loadService(path)
}

// LoadFile 使用显式 base 路径解析并校验存储。
func LoadFile(path string) (Config, error) {
	if path == "" {
		return Config{}, errors.New("CONFIG_READ_FAILED")
	}
	return loadService(path)
}

func loadService(path string) (Config, error) {
	c, err := load(path)
	if err != nil {
		return Config{}, err
	}
	if err := c.ValidateStorage(); err != nil {
		return Config{}, err
	}
	return c, nil
}

// LoadPublic 使用默认发现路径解析，只返回公共配置，不要求存储秘密。
func LoadPublic() (Public, error) {
	path, err := discoverPath()
	if err != nil {
		return Public{}, err
	}
	return loadPublic(path)
}

func loadPublic(path string) (Public, error) {
	c, err := load(path)
	if err != nil {
		return Public{}, err
	}
	return c.Public(), nil
}

// LoadPublicFile 使用显式 base 路径解析，只返回公共配置。
func LoadPublicFile(path string) (Public, error) {
	if path == "" {
		return Public{}, errors.New("CONFIG_READ_FAILED")
	}
	c, err := load(path)
	if err != nil {
		return Public{}, err
	}
	return c.Public(), nil
}

// discoverPath 区分「未设置」与「显式空」：显式设置但为空必须拒绝，不得回退默认。
func discoverPath() (string, error) {
	if v, ok := os.LookupEnv("POKER_CONFIG_FILE"); ok {
		if v == "" {
			return "", errors.New("CONFIG_READ_FAILED")
		}
		return v, nil
	}
	return DefaultPath, nil
}

// load 是唯一的解析器：base → 同目录 local → 白名单环境变量。
func load(base string) (Config, error) {
	if base == "" {
		base = DefaultPath
	}
	raw, found, err := loadFile(base, true)
	if err != nil {
		return Config{}, err
	}
	if !found {
		return Config{}, errors.New("CONFIG_READ_FAILED")
	}
	if localPath := filepath.Join(filepath.Dir(base), LocalName); localPath != base {
		local, localFound, lerr := loadFile(localPath, false)
		if lerr != nil {
			return Config{}, lerr
		}
		if localFound {
			mergeFile(raw, local)
		}
	}
	c := defaults()
	applyFile(&c, raw)
	if err := applyEnv(&c); err != nil {
		return Config{}, err
	}
	if err := c.finalize(); err != nil {
		return Config{}, err
	}
	return c, nil
}

func defaults() Config {
	return Config{
		HTTPHost:                defaultHost,
		HTTPPort:                defaultPort,
		HTTPTimeout:             defaultHTTPTimeout,
		FrontendPort:            defaultFrontendPort,
		RedisAddress:            defaultRedisAddress,
		RedisDB:                 defaultRedisDB,
		MaxLiveCalls:            defaultMaxLiveCalls,
		DecisionTimeout:         defaultRequestTmo,
		MaxOutputTokens:         defaultMaxTokens,
		HumanActionTimeout:      defaultHumanAction,
		AITurnDelay:             defaultAITurnDelay,
		HandIntervalShowdown:    defaultHandIntervalShowdown,
		HandIntervalUncontested: defaultHandIntervalUncontested,
		dsnEnv:                  defaultDSNEnv,
		passwordEnv:             defaultRedisPassEnv,
		catalogRaw:              autoCatalog,
	}
}

// loadFile 读取并严格解析单个文件。
//   - found=false：文件不存在（非错误，由调用方决定 required 语义）。
//   - required=true（base）：空文件 / 全注释 / 空 root mapping 一律 CONFIG_INVALID。
//   - required=false（local）：空文件 / 全注释 / 空 mapping 视为 noop；root null / 字段 null 拒绝。
func loadFile(path string, required bool) (*fileConfig, bool, error) {
	root, found, err := readRoot(path)
	if err != nil {
		return nil, false, err
	}
	if !found {
		return nil, false, nil
	}
	if root == nil {
		if required {
			return nil, false, errors.New("CONFIG_INVALID")
		}
		return &fileConfig{}, true, nil
	}
	if err := validateNode(root, configSchema); err != nil {
		return nil, false, err
	}
	if root.Kind == yaml.MappingNode && len(root.Content) == 0 && required {
		return nil, false, errors.New("CONFIG_INVALID")
	}
	var raw fileConfig
	if err := root.Decode(&raw); err != nil {
		return nil, false, errors.New("CONFIG_INVALID")
	}
	return &raw, true, nil
}

// readRoot 解析单个 YAML 文档并拒绝多文档流。root=nil 表示空文档（空文件或全注释）。
func readRoot(path string) (*yaml.Node, bool, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, errors.New("CONFIG_READ_FAILED")
	}
	if len(b) > maxFileBytes {
		return nil, false, errors.New("CONFIG_INVALID")
	}
	dec := yaml.NewDecoder(bytes.NewReader(b))
	var doc yaml.Node
	if err := dec.Decode(&doc); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, true, nil
		}
		return nil, false, errors.New("CONFIG_INVALID")
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, false, errors.New("CONFIG_INVALID")
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return nil, true, nil
	}
	return doc.Content[0], true, nil
}

type nodeKind int

const (
	kindMap nodeKind = iota
	kindString
	kindDuration
	kindInt
	kindUint
	kindBool
)

type fieldSchema struct {
	kind     nodeKind
	children map[string]*fieldSchema
}

// configSchema 是冻结的严格 schema：未知字段、非 mapping 层、类型不符、null、重复键、
// 别名与合并键一律拒绝。YAML 别名/合并键明确不支持，避免 Node 循环。
var configSchema = &fieldSchema{kind: kindMap, children: map[string]*fieldSchema{
	"server": {kind: kindMap, children: map[string]*fieldSchema{
		"http": {kind: kindMap, children: map[string]*fieldSchema{
			"host":    {kind: kindString},
			"port":    {kind: kindInt},
			"timeout": {kind: kindDuration},
		}},
	}},
	"frontend": {kind: kindMap, children: map[string]*fieldSchema{
		"dev_port": {kind: kindInt},
	}},
	"data": {kind: kindMap, children: map[string]*fieldSchema{
		"mysql": {kind: kindMap, children: map[string]*fieldSchema{
			"dsn":     {kind: kindString},
			"dsn_env": {kind: kindString},
		}},
		"redis": {kind: kindMap, children: map[string]*fieldSchema{
			"addr":         {kind: kindString},
			"db":           {kind: kindInt},
			"password":     {kind: kindString},
			"password_env": {kind: kindString},
		}},
	}},
	"agents": {kind: kindMap, children: map[string]*fieldSchema{
		"catalog_path":      {kind: kindString},
		"request_timeout":   {kind: kindDuration},
		"max_output_tokens": {kind: kindInt},
		"allow_paid_models": {kind: kindBool},
		"max_live_calls":    {kind: kindUint},
	}},
	"gameplay": {kind: kindMap, children: map[string]*fieldSchema{
		"human_action_timeout":          {kind: kindDuration},
		"ai_turn_delay":                 {kind: kindDuration},
		"hand_interval_showdown_sec":    {kind: kindInt},
		"hand_interval_uncontested_sec": {kind: kindInt},
	}},
}}

// validateNode 只接受与 schema 完全匹配的标量 tag；显式 null（!!null）不被当作 missing。
func validateNode(n *yaml.Node, schema *fieldSchema) error {
	if n == nil || n.Kind == yaml.AliasNode {
		return errors.New("CONFIG_INVALID")
	}
	switch schema.kind {
	case kindMap:
		if n.Kind != yaml.MappingNode {
			return errors.New("CONFIG_INVALID")
		}
		seen := make(map[string]bool, len(n.Content)/2)
		for i := 0; i+1 < len(n.Content); i += 2 {
			key, val := n.Content[i], n.Content[i+1]
			if key.Kind != yaml.ScalarNode || key.Tag != "!!str" {
				return errors.New("CONFIG_INVALID")
			}
			if key.Value == "<<" || key.Tag == "!!merge" {
				return errors.New("CONFIG_INVALID")
			}
			if seen[key.Value] {
				return errors.New("CONFIG_INVALID")
			}
			seen[key.Value] = true
			child, ok := schema.children[key.Value]
			if !ok {
				return errors.New("CONFIG_INVALID")
			}
			if err := validateNode(val, child); err != nil {
				return err
			}
		}
		return nil
	case kindString:
		return expectScalar(n, "!!str")
	case kindDuration:
		if n.Kind != yaml.ScalarNode || n.Tag != "!!str" {
			return errors.New("CONFIG_INVALID")
		}
		if _, err := time.ParseDuration(n.Value); err != nil {
			return errors.New("CONFIG_INVALID")
		}
		return nil
	case kindInt, kindUint:
		return expectScalar(n, "!!int")
	case kindBool:
		return expectScalar(n, "!!bool")
	}
	return errors.New("CONFIG_INVALID")
}

func expectScalar(n *yaml.Node, tag string) error {
	if n.Kind == yaml.ScalarNode && n.Tag == tag {
		return nil
	}
	return errors.New("CONFIG_INVALID")
}

// mergeFile 将 src 中显式出现的字段覆盖到 dst，保留显式 false / 0。
func mergeFile(dst, src *fileConfig) {
	if src.Server.HTTP.Host != nil {
		dst.Server.HTTP.Host = src.Server.HTTP.Host
	}
	if src.Server.HTTP.Port != nil {
		dst.Server.HTTP.Port = src.Server.HTTP.Port
	}
	if src.Server.HTTP.Timeout != nil {
		dst.Server.HTTP.Timeout = src.Server.HTTP.Timeout
	}
	if src.Frontend.DevPort != nil {
		dst.Frontend.DevPort = src.Frontend.DevPort
	}
	if src.Data.MySQL.DSN != nil {
		dst.Data.MySQL.DSN = src.Data.MySQL.DSN
	}
	if src.Data.MySQL.DSNEnv != nil {
		dst.Data.MySQL.DSNEnv = src.Data.MySQL.DSNEnv
	}
	if src.Data.Redis.Addr != nil {
		dst.Data.Redis.Addr = src.Data.Redis.Addr
	}
	if src.Data.Redis.DB != nil {
		dst.Data.Redis.DB = src.Data.Redis.DB
	}
	if src.Data.Redis.Password != nil {
		dst.Data.Redis.Password = src.Data.Redis.Password
	}
	if src.Data.Redis.PasswordEnv != nil {
		dst.Data.Redis.PasswordEnv = src.Data.Redis.PasswordEnv
	}
	if src.Agents.CatalogPath != nil {
		dst.Agents.CatalogPath = src.Agents.CatalogPath
	}
	if src.Agents.RequestTimeout != nil {
		dst.Agents.RequestTimeout = src.Agents.RequestTimeout
	}
	if src.Agents.MaxOutputTokens != nil {
		dst.Agents.MaxOutputTokens = src.Agents.MaxOutputTokens
	}
	if src.Agents.AllowPaidModels != nil {
		dst.Agents.AllowPaidModels = src.Agents.AllowPaidModels
	}
	if src.Agents.MaxLiveCalls != nil {
		dst.Agents.MaxLiveCalls = src.Agents.MaxLiveCalls
	}
	if src.Gameplay.HumanActionTimeout != nil {
		dst.Gameplay.HumanActionTimeout = src.Gameplay.HumanActionTimeout
	}
	if src.Gameplay.AITurnDelay != nil {
		dst.Gameplay.AITurnDelay = src.Gameplay.AITurnDelay
	}
	if src.Gameplay.HandIntervalShowdown != nil {
		dst.Gameplay.HandIntervalShowdown = src.Gameplay.HandIntervalShowdown
	}
	if src.Gameplay.HandIntervalUncontested != nil {
		dst.Gameplay.HandIntervalUncontested = src.Gameplay.HandIntervalUncontested
	}
}

func applyFile(c *Config, raw *fileConfig) {
	if raw.Server.HTTP.Host != nil {
		c.HTTPHost = *raw.Server.HTTP.Host
	}
	if raw.Server.HTTP.Port != nil {
		c.HTTPPort = *raw.Server.HTTP.Port
	}
	if raw.Server.HTTP.Timeout != nil {
		c.HTTPTimeout = *raw.Server.HTTP.Timeout
	}
	if raw.Frontend.DevPort != nil {
		c.FrontendPort = *raw.Frontend.DevPort
	}
	if raw.Data.MySQL.DSN != nil {
		c.dsnDirect = *raw.Data.MySQL.DSN
	}
	if raw.Data.MySQL.DSNEnv != nil {
		c.dsnEnv = *raw.Data.MySQL.DSNEnv
	}
	if raw.Data.Redis.Addr != nil {
		c.RedisAddress = *raw.Data.Redis.Addr
	}
	if raw.Data.Redis.DB != nil {
		c.RedisDB = *raw.Data.Redis.DB
	}
	if raw.Data.Redis.Password != nil {
		c.passwordDirect = *raw.Data.Redis.Password
		c.passwordPresent = true
	}
	if raw.Data.Redis.PasswordEnv != nil {
		c.passwordEnv = *raw.Data.Redis.PasswordEnv
	}
	if raw.Agents.CatalogPath != nil {
		c.catalogRaw = *raw.Agents.CatalogPath
	}
	if raw.Agents.RequestTimeout != nil {
		c.DecisionTimeout = *raw.Agents.RequestTimeout
	}
	if raw.Agents.MaxOutputTokens != nil {
		c.MaxOutputTokens = *raw.Agents.MaxOutputTokens
	}
	if raw.Agents.AllowPaidModels != nil {
		c.AllowPaidModels = *raw.Agents.AllowPaidModels
	}
	if raw.Agents.MaxLiveCalls != nil {
		c.MaxLiveCalls = *raw.Agents.MaxLiveCalls
	}
	if raw.Gameplay.HumanActionTimeout != nil {
		c.HumanActionTimeout = *raw.Gameplay.HumanActionTimeout
	}
	if raw.Gameplay.AITurnDelay != nil {
		c.AITurnDelay = *raw.Gameplay.AITurnDelay
	}
	if raw.Gameplay.HandIntervalShowdown != nil {
		c.HandIntervalShowdown = time.Duration(*raw.Gameplay.HandIntervalShowdown) * time.Second
	}
	if raw.Gameplay.HandIntervalUncontested != nil {
		c.HandIntervalUncontested = time.Duration(*raw.Gameplay.HandIntervalUncontested) * time.Second
	}
}

// applyEnv 只接受非空环境变量作为显式覆盖；兼容旧键并新增 HTTP/前端/agent 限额键。
func applyEnv(c *Config) error {
	if v, ok := os.LookupEnv("POKER_HTTP_HOST"); ok && v != "" {
		c.HTTPHost = v
	}
	if v, ok := os.LookupEnv("POKER_HTTP_PORT"); ok && v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return errors.New("CONFIG_INVALID")
		}
		c.HTTPPort = n
	}
	if v, ok := os.LookupEnv("POKER_HTTP_TIMEOUT"); ok && v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return errors.New("CONFIG_INVALID")
		}
		c.HTTPTimeout = d
	}
	if v, ok := os.LookupEnv("POKER_FRONTEND_PORT"); ok && v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return errors.New("CONFIG_INVALID")
		}
		c.FrontendPort = n
	}
	if v, ok := os.LookupEnv("POKER_REDIS_ADDR"); ok && v != "" {
		c.RedisAddress = v
	}
	if v, ok := os.LookupEnv("POKER_REDIS_DB"); ok && v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 || n > 15 {
			return errors.New("CONFIG_INVALID")
		}
		c.RedisDB = n
	}
	if v, ok := os.LookupEnv("POKER_CATALOG_PATH"); ok && v != "" {
		c.catalogRaw = v
	}
	if v, ok := os.LookupEnv("POKER_ALLOW_PAID_MODELS"); ok && v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return errors.New("CONFIG_INVALID")
		}
		c.AllowPaidModels = b
	}
	if v, ok := os.LookupEnv("POKER_MAX_LIVE_CALLS"); ok && v != "" {
		n, err := strconv.ParseUint(v, 10, 32)
		if err != nil || n == 0 || n > maxLiveCallsLimit {
			return errors.New("CONFIG_INVALID")
		}
		c.MaxLiveCalls = uint32(n)
	}
	if v, ok := os.LookupEnv("POKER_DECISION_TIMEOUT"); ok && v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return errors.New("CONFIG_INVALID")
		}
		c.DecisionTimeout = d
	}
	if v, ok := os.LookupEnv("POKER_MAX_OUTPUT_TOKENS"); ok && v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 || n > maxOutputTokensLimit {
			return errors.New("CONFIG_INVALID")
		}
		c.MaxOutputTokens = n
	}
	if v, ok := os.LookupEnv("POKER_HUMAN_ACTION_TIMEOUT"); ok && v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return errors.New("CONFIG_INVALID")
		}
		c.HumanActionTimeout = d
	}
	if v, ok := os.LookupEnv("POKER_AI_TURN_DELAY"); ok && v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return errors.New("CONFIG_INVALID")
		}
		c.AITurnDelay = d
	}
	if v, ok := os.LookupEnv("POKER_HAND_INTERVAL_SHOWDOWN_SEC"); ok && v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return errors.New("CONFIG_INVALID")
		}
		c.HandIntervalShowdown = time.Duration(n) * time.Second
	}
	if v, ok := os.LookupEnv("POKER_HAND_INTERVAL_UNCONTESTED_SEC"); ok && v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return errors.New("CONFIG_INVALID")
		}
		c.HandIntervalUncontested = time.Duration(n) * time.Second
	}
	return nil
}

var (
	envNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	hostLabel      = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?$`)
)

// validHost 只接受裸合法 IP 或合法主机名；拒绝 path/scheme/userinfo/端口/空白，不修剪猜测。
func validHost(host string) bool {
	if host == "" || strings.TrimSpace(host) != host {
		return false
	}
	if net.ParseIP(host) != nil {
		return true
	}
	if len(host) > 253 || strings.ContainsAny(host, "/:@?#\\[]") {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if !hostLabel.MatchString(label) {
			return false
		}
	}
	return true
}

func (c *Config) finalize() error {
	if !validHost(c.HTTPHost) {
		return errors.New("CONFIG_INVALID")
	}
	if !envNamePattern.MatchString(c.dsnEnv) || !envNamePattern.MatchString(c.passwordEnv) {
		return errors.New("CONFIG_INVALID")
	}
	// 显式 catalog_path: "" 拒绝；未填（默认 auto）才允许自动发现。
	if c.catalogRaw == "" {
		return errors.New("CONFIG_INVALID")
	}
	if c.HTTPPort < 1 || c.HTTPPort > 65535 {
		return errors.New("CONFIG_INVALID")
	}
	if c.FrontendPort < 1 || c.FrontendPort > 65535 {
		return errors.New("CONFIG_INVALID")
	}
	if c.HTTPTimeout <= 0 || c.DecisionTimeout <= 0 {
		return errors.New("CONFIG_INVALID")
	}
	if c.MaxOutputTokens <= 0 || c.MaxOutputTokens > maxOutputTokensLimit {
		return errors.New("CONFIG_INVALID")
	}
	if c.RedisDB < 0 || c.RedisDB > 15 {
		return errors.New("CONFIG_INVALID")
	}
	if c.MaxLiveCalls == 0 || c.MaxLiveCalls > maxLiveCallsLimit {
		return errors.New("CONFIG_INVALID")
	}
	if c.HumanActionTimeout <= 0 {
		return errors.New("CONFIG_INVALID")
	}
	if c.AITurnDelay <= 0 {
		return errors.New("CONFIG_INVALID")
	}
	if c.HandIntervalShowdown <= 0 || c.HandIntervalUncontested <= 0 {
		return errors.New("CONFIG_INVALID")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return errors.New("CONFIG_HOME_UNAVAILABLE")
	}
	c.Address = net.JoinHostPort(c.HTTPHost, strconv.Itoa(c.HTTPPort))
	c.CatalogPath = resolveCatalog(c.catalogRaw, home)
	// 秘密只在进程内解析，不落盘、不输出。
	// dsn：直连非空优先；空串等同未设置，回退 dsn_env 指向的环境变量。
	// password：直连字段存在即以它为准（空串 = 无密码）；缺省才读 password_env。
	if c.dsnDirect != "" {
		c.MySQLDSN = c.dsnDirect
	} else {
		c.MySQLDSN = os.Getenv(c.dsnEnv)
	}
	if c.passwordPresent {
		c.RedisPassword = c.passwordDirect
	} else {
		c.RedisPassword = os.Getenv(c.passwordEnv)
	}
	return nil
}

// resolveCatalog 处理 catalog_path: auto：优先 opencode.jsonc，其次 opencode.json；
// 显式路径原样保留（即使文件不存在，交由 agent.LoadCatalog 报安全错误）。
func resolveCatalog(raw, home string) string {
	if raw != "" && raw != autoCatalog {
		return raw
	}
	base := filepath.Join(home, ".config", "opencode")
	jsonc := filepath.Join(base, "opencode.jsonc")
	if _, err := os.Stat(jsonc); err == nil {
		return jsonc
	}
	json := filepath.Join(base, "opencode.json")
	if _, err := os.Stat(json); err == nil {
		return json
	}
	return jsonc
}
