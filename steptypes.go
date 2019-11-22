package main

import (
	"github.com/kong/go-pdk/client"
	"github.com/kong/go-pdk/entities"
	"github.com/kong/go-pdk/node"
)

type Error string
type StepErrorData struct {
	EventId int
	Data    Error
}

func (s *PluginServer) StepError(in StepErrorData, out *StepData) error {
	return s.Step(StepData{
		EventId: in.EventId,
		Data:    in.Data,
	}, out)
}

type StepCredentialData struct {
	EventId int
	Data    client.AuthenticatedCredential
}

func (s *PluginServer) StepCredential(in StepCredentialData, out *StepData) error {
	return s.Step(StepData{
		EventId: in.EventId,
		Data:    in.Data,
	}, out)
}

type StepRouteData struct {
	EventId int
	Data    entities.Route
}

func (s *PluginServer) StepRoute(in StepRouteData, out *StepData) error {
	return s.Step(StepData{
		EventId: in.EventId,
		Data:    in.Data,
	}, out)
}

type StepServiceData struct {
	EventId int
	Data    entities.Service
}

func (s *PluginServer) StepService(in StepServiceData, out *StepData) error {
	return s.Step(StepData{
		EventId: in.EventId,
		Data:    in.Data,
	}, out)
}

type StepConsumerData struct {
	EventId int
	Data    entities.Consumer
}

func (s *PluginServer) StepConsumer(in StepConsumerData, out *StepData) error {
	return s.Step(StepData{
		EventId: in.EventId,
		Data:    in.Data,
	}, out)
}

type StepMemoryStatsData struct {
	EventId int
	Data    node.MemoryStats
}

func (s *PluginServer) StepMemoryStats(in StepMemoryStatsData, out *StepData) error {
	return s.Step(StepData{
		EventId: in.EventId,
		Data:    in.Data,
	}, out)
}
