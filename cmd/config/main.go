// Package main 是配置自检 CLI：只输出严格白名单 JSON，不依赖 web/server，不输出任何凭据。
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"texas-holdem/internal/conf"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("config", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	path := fs.String("conf", "", "配置文件路径（默认 POKER_CONFIG_FILE 或 config/config.yaml）")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("INVALID_COMMAND")
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("INVALID_COMMAND")
	}
	explicit := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "conf" {
			explicit = true
		}
	})
	var (
		p   conf.Public
		err error
	)
	if explicit {
		// 显式 -conf "" 必须拒绝，不得回退到 POKER_CONFIG_FILE 或默认。
		p, err = conf.LoadPublicFile(*path)
	} else {
		p, err = conf.LoadPublic()
	}
	if err != nil {
		return err
	}
	b, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("CONFIG_INVALID")
	}
	_, err = fmt.Fprintln(stdout, string(b))
	return err
}
