package core

import (
	"context"

	eventbus "github.com/protocol/hack-the-bus"
)

type Context struct {
	ctx                     context.Context
	publishEvent            func(eventbus.EventType, eventbus.EventData) error
	publishSynchronousEvent func(eventbus.EventType, eventbus.EventData) error
}

func (ctx *Context) Context() context.Context {
	return ctx.ctx
}

func (ctx *Context) PublishEvent(eventType eventbus.EventType, eventData eventbus.EventData) error {
	return ctx.publishEvent(eventType, eventData)
}

func (ctx *Context) PublishSynchronousEvent(eventType eventbus.EventType, eventData eventbus.EventData) error {
	return ctx.publishSynchronousEvent(eventType, eventData)
}
