package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/go-kratos/kratos/v3"
	"io"
	"os"
	"texas-holdem/internal/agent"
	"texas-holdem/internal/conf"
	"texas-holdem/internal/data"
	"texas-holdem/internal/server"
	"texas-holdem/internal/service"
	"texas-holdem/web"
	"time"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}

// parseArgs 解析 -conf 与可选 migrate 子命令；显式 -conf "" 由 resolveConfig 拒绝。
func parseArgs(args []string) (confPath string, explicit, migrate bool, err error) {
	fs := flag.NewFlagSet("server", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	path := fs.String("conf", "", "配置文件路径（默认 POKER_CONFIG_FILE 或 config/config.yaml）")
	if e := fs.Parse(args); e != nil {
		return "", false, false, fmt.Errorf("INVALID_COMMAND")
	}
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "conf" {
			explicit = true
		}
	})
	rest := fs.Args()
	if len(rest) > 1 || (len(rest) == 1 && rest[0] != "migrate") {
		return "", false, false, fmt.Errorf("INVALID_COMMAND")
	}
	return *path, explicit, len(rest) == 1, nil
}

func resolveConfig(path string, explicit bool) (conf.Config, error) {
	if explicit {
		return conf.LoadFile(path)
	}
	return conf.Load()
}

func run(args []string) error {
	confPath, explicit, migrate, err := parseArgs(args)
	if err != nil {
		return err
	}
	c, err := resolveConfig(confPath, explicit)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if migrate {
		return data.Migrate(ctx, c)
	}
	repo, err := data.Open(ctx, c, server.MarshalGame)
	if err != nil {
		return err
	}
	defer func() {
		if err := repo.Close(); err != nil {
			fmt.Fprintln(os.Stderr, "STORAGE_CLOSE_FAILED")
		}
	}()
	// 配置只在进程内部加载；不会打印原始解析、SDK 或配置错误。
	catalog, err := agent.LoadCatalog(c.CatalogPath)
	if err != nil {
		return fmt.Errorf("MODEL_CATALOG_INVALID")
	}
	factory, err := agent.NewFactoryWithOptions(catalog, agent.Options{RequestTimeout: c.DecisionTimeout, MaxOutputTokens: c.MaxOutputTokens})
	if err != nil {
		return fmt.Errorf("MODEL_FACTORY_INVALID")
	}
	manager, err := service.New(context.Background(), repo, factory, service.Options{AllowPaidModels: c.AllowPaidModels, MaxLiveCalls: c.MaxLiveCalls, DecisionTimeout: c.DecisionTimeout, HumanActionTimeout: c.HumanActionTimeout, AITurnDelay: c.AITurnDelay})
	if err != nil {
		return err
	}
	defer manager.Close()
	assets, err := web.Assets()
	if err != nil {
		return fmt.Errorf("WEB_ASSETS_UNAVAILABLE")
	}
	app := kratos.New(kratos.Name("texas-holdem"), kratos.Server(server.New(manager, assets, server.Options{Host: c.HTTPHost, Port: c.HTTPPort, Timeout: c.HTTPTimeout})))
	if err = app.Run(); err != nil {
		return fmt.Errorf("SERVER_LIFECYCLE_FAILED")
	}
	return nil
}
