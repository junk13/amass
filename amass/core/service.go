// Copyright 2017 Jeff Foley. All rights reserved.
// Use of this source code is governed by Apache 2 LICENSE that can be found in the LICENSE file.

package core

import (
	"errors"
	"sync"
	"time"
)

// AmassService is the object type for a service running within the Amass enumeration architecture.
type AmassService interface {
	// Start the service
	Start() error
	OnStart() error

	// OPSEC for the service
	List() string

	// Pause the service
	Pause() error
	OnPause() error

	// Resume the service
	Resume() error
	OnResume() error

	// Stop the service
	Stop() error
	OnStop() error

	SendRequest(req *AmassRequest)
	RequestChan() <-chan *AmassRequest

	IsActive() bool
	SetActive()

	// Returns channels that fire during Pause/Resume operations
	PauseChan() <-chan struct{}
	ResumeChan() <-chan struct{}

	// Returns a channel that is closed when the service is stopped
	Quit() <-chan struct{}

	// String description of the service
	String() string

	// Returns the enumeration configuration
	Config() *AmassConfig
}

// BaseAmassService provides common mechanisms to all Amass services in the enumeration architecture.
// It is used to compose a type that completely meets the AmassService interface.
type BaseAmassService struct {
	sync.Mutex
	name    string
	started bool
	stopped bool
	active  time.Time
	queue   chan *AmassRequest
	pause   chan struct{}
	resume  chan struct{}
	quit    chan struct{}
	config  *AmassConfig

	// The specific service embedding BaseAmassService
	service AmassService
}

// NewBaseAmassService returns an initialized BaseAmassService object.
func NewBaseAmassService(name string, config *AmassConfig, service AmassService) *BaseAmassService {
	return &BaseAmassService{
		name:    name,
		active:  time.Now(),
		queue:   make(chan *AmassRequest, 100),
		pause:   make(chan struct{}),
		resume:  make(chan struct{}),
		quit:    make(chan struct{}),
		config:  config,
		service: service,
	}
}

// Start calls the OnStart method implemented for the AmassService.
func (bas *BaseAmassService) Start() error {
	if bas.isStarted() {
		return errors.New(bas.name + " service has already been started")
	} else if bas.isStopped() {
		return errors.New(bas.name + " service has been stopped")
	}
	bas.started = true
	return bas.service.OnStart()
}

// OnStart is a placeholder that should be implemented by an AmassService
// that has code to execute during service start.
func (bas *BaseAmassService) OnStart() error {
	return nil
}

// List implements the AmassService interface
func (bas *BaseAmassService) List() string {
	return "N/A"
}

// Pause implements the AmassService interface
func (bas *BaseAmassService) Pause() error {
	err := bas.service.OnPause()

	go func() {
		bas.pause <- struct{}{}
	}()
	return err
}

// OnPause implements the AmassService interface
func (bas *BaseAmassService) OnPause() error {
	return nil
}

// Resume implements the AmassService interface
func (bas *BaseAmassService) Resume() error {
	err := bas.service.OnResume()

	go func() {
		bas.resume <- struct{}{}
	}()
	return err
}

// OnResume implements the AmassService interface
func (bas *BaseAmassService) OnResume() error {
	return nil
}

// Stop alls the OnStop method implemented for the AmassService.
func (bas *BaseAmassService) Stop() error {
	if bas.isStopped() {
		return errors.New(bas.name + " service has already been stopped")
	}
	bas.Resume()
	err := bas.service.OnStop()
	bas.stopped = true
	close(bas.quit)
	return err
}

// OnStop is a placeholder that should be implemented by an AmassService
// that has code to execute during service stop.
func (bas *BaseAmassService) OnStop() error {
	return nil
}

// NumOfRequests returns the current length of the service request queue.
func (bas *BaseAmassService) NumOfRequests() int {
	bas.Lock()
	defer bas.Unlock()

	return len(bas.queue)
}

// SendRequest adds the request provided by the parameter to the service request channel.
func (bas *BaseAmassService) SendRequest(req *AmassRequest) {
	go func() {
		bas.queue <- req
	}()
}

// RequestChan returns the channel that provides new service requests.
func (bas *BaseAmassService) RequestChan() <-chan *AmassRequest {
	return bas.queue
}

// IsActive returns true if SetActive has been called for the service within the last 10 seconds.
func (bas *BaseAmassService) IsActive() bool {
	bas.Lock()
	defer bas.Unlock()

	if time.Now().Sub(bas.active) > 10*time.Second {
		return false
	}
	return true
}

// SetActive marks the service as being active at time.Now() for future checks performed by the IsActive method.
func (bas *BaseAmassService) SetActive() {
	bas.Lock()
	defer bas.Unlock()

	bas.active = time.Now()
}

// PauseChan returns the pause channel for the service.
func (bas *BaseAmassService) PauseChan() <-chan struct{} {
	return bas.pause
}

// ResumeChan returns the resume channel for the service.
func (bas *BaseAmassService) ResumeChan() <-chan struct{} {
	return bas.resume
}

// Quit return the quit channel for the service.
func (bas *BaseAmassService) Quit() <-chan struct{} {
	return bas.quit
}

// String returns the name of the service.
func (bas *BaseAmassService) String() string {
	return bas.name
}

// Config returns the Amass enumeration configuration that was provided to the AmassService.
func (bas *BaseAmassService) Config() *AmassConfig {
	return bas.config
}

func (bas *BaseAmassService) isStarted() bool {
	bas.Lock()
	defer bas.Unlock()

	return bas.started
}

func (bas *BaseAmassService) isStopped() bool {
	bas.Lock()
	defer bas.Unlock()

	return bas.stopped
}
