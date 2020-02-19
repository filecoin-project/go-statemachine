package statemachine

import "context"

type Context struct {
	ctx  context.Context
	name interface{}
	send func(evt interface{}) error
}

func (ctx *Context) Name() interface{} {
	return ctx.name
}

func (ctx *Context) Context() context.Context {
	return ctx.ctx
}

func (ctx *Context) Send(evt interface{}) error {
	return ctx.send(evt)
}
