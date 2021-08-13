package statemachine

import (
	"context"

	core "github.com/filecoin-project/go-statemachine/internal"
)

type Context struct {
	systemName string
	ctx        context.Context
	sm         *core.StateMachine
}

func (ctx *Context) Context() context.Context {
	return ctx.ctx
}

func (ctx *Context) Send(evt interface{}) error {
	return ctx.sm.PublishEvent(internalEvent{ctx.systemName}, Event{User: evt})
}
