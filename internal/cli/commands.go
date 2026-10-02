package cli

import (
	"context"
	"strings"
	"time"

	wonton "github.com/deepnoodle-ai/wonton/cli"
)

// commandBinding connects Wonton's typed flag values to the command's options.
// Wonton owns parsing, routing, argument validation, and help generation.
type commandBinding struct {
	name    string
	flags   []wonton.Flag
	bind    []func(*wonton.Context)
	handler func(context.Context, []string) int
	context *wonton.Context
}

func (a *App) flags(name string) *commandBinding {
	return &commandBinding{name: name}
}

func (b *commandBinding) run(fn func(context.Context, []string) int) *commandBinding {
	b.handler = fn
	return b
}

func (b *commandBinding) attach(command *wonton.Command) {
	command.Flags(b.flags...)
	command.Run(func(ctx *wonton.Context) error {
		b.context = ctx
		for _, bind := range b.bind {
			bind(ctx)
		}
		if code := b.handler(ctx.Context(), ctx.Args()); code != 0 {
			return wonton.Exit(code)
		}
		return nil
	})
}

func (b *commandBinding) StringVar(p *string, name, value, help string) {
	b.flags = append(b.flags, wonton.String(name).Default(value).Help(help))
	b.bind = append(b.bind, func(c *wonton.Context) { *p = c.String(name) })
}

func (b *commandBinding) IntVar(p *int, name string, value int, help string) {
	b.flags = append(b.flags, wonton.Int(name).Default(value).Help(help))
	b.bind = append(b.bind, func(c *wonton.Context) { *p = c.Int(name) })
}

func (b *commandBinding) Int64Var(p *int64, name string, value int64, help string) {
	b.flags = append(b.flags, wonton.Int64(name).Default(value).Help(help))
	b.bind = append(b.bind, func(c *wonton.Context) { *p = c.Int64(name) })
}

func (b *commandBinding) BoolVar(p *bool, name string, value bool, help string) {
	b.flags = append(b.flags, wonton.Bool(name).Default(value).Help(help))
	b.bind = append(b.bind, func(c *wonton.Context) { *p = c.Bool(name) })
}

func (b *commandBinding) Bool(name string, value bool, help string) *bool {
	p := new(bool)
	b.BoolVar(p, name, value, help)
	return p
}

func (b *commandBinding) Float64Var(p *float64, name string, value float64, help string) {
	b.flags = append(b.flags, wonton.Float64(name).Default(value).Help(help))
	b.bind = append(b.bind, func(c *wonton.Context) { *p = c.Float64(name) })
}

func (b *commandBinding) DurationVar(p *time.Duration, name string, value time.Duration, help string) {
	b.flags = append(b.flags, wonton.Duration(name).Default(value).Help(help))
	b.bind = append(b.bind, func(c *wonton.Context) { *p = c.Duration(name) })
}

func (b *commandBinding) StringsVar(p *[]string, name, help string) {
	b.flags = append(b.flags, wonton.Strings(name).Default((*p)...).Help(help))
	b.bind = append(b.bind, func(c *wonton.Context) { *p = c.Strings(name) })
}

func (b *commandBinding) attachGroup(group *wonton.Group) {
	group.Flags(b.flags...).Run(func(ctx *wonton.Context) error {
		if ctx.NArg() != 0 {
			return wonton.Errorf("unknown %s command %q", strings.Fields(b.name)[0], ctx.Arg(0))
		}
		b.context = ctx
		for _, bind := range b.bind {
			bind(ctx)
		}
		if code := b.handler(ctx.Context(), nil); code != 0 {
			return wonton.Exit(code)
		}
		return nil
	})
}
