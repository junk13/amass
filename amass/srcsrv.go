// Copyright 2017 Jeff Foley. All rights reserved.
// Use of this source code is governed by Apache 2 LICENSE that can be found in the LICENSE file.

package amass

import (
	"regexp"
	"strings"
	"time"

	"github.com/OWASP/Amass/amass/core"
	"github.com/OWASP/Amass/amass/sources"
	"github.com/OWASP/Amass/amass/utils"
	evbus "github.com/asaskevich/EventBus"
)

var (
	nameStripRE = regexp.MustCompile("^((20)|(25)|(2f)|(3d)|(40))+")
)

type entry struct {
	Source sources.DataSource
	Domain string
	Sub    string
}

// SourcesService is the AmassService that handles the querying of all data sources
// within the architecture. This is achieved by receiving all the RESOLVED events.
type SourcesService struct {
	core.BaseAmassService

	bus           evbus.Bus
	responses     chan *core.AmassRequest
	directs       []sources.DataSource
	throttles     []sources.DataSource
	throttleQueue []*entry
	filter        *utils.StringFilter
	outfilter     *utils.StringFilter
}

// NewSourcesService requires the enumeration configuration and event bus as parameters.
// The object returned is initialized, but has not yet been started.
func NewSourcesService(config *core.AmassConfig, bus evbus.Bus) *SourcesService {
	ss := &SourcesService{
		bus:       bus,
		responses: make(chan *core.AmassRequest, 50),
		filter:    utils.NewStringFilter(),
		outfilter: utils.NewStringFilter(),
	}
	ss.BaseAmassService = *core.NewBaseAmassService("Sources Service", config, ss)

	for _, source := range sources.GetAllSources(ss) {
		if source.Type() == core.ARCHIVE {
			ss.throttles = append(ss.throttles, source)
		} else {
			ss.directs = append(ss.directs, source)
		}
	}
	return ss
}

// OnStart implements the AmassService interface
func (ss *SourcesService) OnStart() error {
	ss.BaseAmassService.OnStart()

	ss.bus.SubscribeAsync(core.CHECKED, ss.SendRequest, false)
	go ss.processRequests()
	go ss.processOutput()
	go ss.processThrottleQueue()
	go ss.queryAllSources()
	return nil
}

// OnStop implements the AmassService interface
func (ss *SourcesService) OnStop() error {
	ss.BaseAmassService.OnStop()

	ss.bus.Unsubscribe(core.CHECKED, ss.SendRequest)
	return nil
}

func (ss *SourcesService) processRequests() {
	for {
		select {
		case <-ss.PauseChan():
			<-ss.ResumeChan()
		case <-ss.Quit():
			return
		case req := <-ss.RequestChan():
			go ss.handleRequest(req)
		}
	}
}

func (ss *SourcesService) handleRequest(req *core.AmassRequest) {
	if ss.filter.Duplicate(req.Name) || !ss.Config().IsDomainInScope(req.Name) {
		return
	}

	var subsrch bool
	if req.Name != req.Domain {
		subsrch = true
	}

	for _, source := range ss.directs {
		if subsrch && !source.Subdomains() {
			continue
		}
		ss.SetActive()
		go ss.queryOneSource(source, req.Domain, req.Name)
	}

	// Do not queue requests that were not resolved
	if len(req.Records) == 0 {
		return
	}

	for _, source := range ss.throttles {
		if subsrch && !source.Subdomains() {
			continue
		}
		ss.throttleAdd(source, req.Domain, req.Name)
	}
}

func (ss *SourcesService) processOutput() {
	for {
		select {
		case req := <-ss.responses:
			go ss.handleOutput(req)
		case <-ss.Quit():
			return
		}
	}
}

func (ss *SourcesService) handleOutput(req *core.AmassRequest) {
	// Clean up the names scraped from the web
	if i := nameStripRE.FindStringIndex(req.Name); i != nil {
		req.Name = req.Name[i[1]:]
	}
	req.Name = strings.TrimSpace(strings.ToLower(req.Name))
	// Remove dots at the beginning of names
	if len(req.Name) > 1 && req.Name[0] == '.' {
		req.Name = req.Name[1:]
	}
	if ss.outfilter.Duplicate(req.Name + req.Source) {
		return
	}
	ss.Config().MaxFlow.Acquire(1)
	ss.bus.Publish(core.NEWNAME, req)
	ss.SendRequest(req)
}

func (ss *SourcesService) queryAllSources() {
	ss.SetActive()

	for _, domain := range ss.Config().Domains() {
		ss.SendRequest(&core.AmassRequest{
			Name:   domain,
			Domain: domain,
		})
	}
}

func (ss *SourcesService) queryOneSource(source sources.DataSource, domain, sub string) {
	for _, name := range source.Query(domain, sub) {
		ss.responses <- &core.AmassRequest{
			Name:   name,
			Domain: domain,
			Tag:    source.Type(),
			Source: source.String(),
		}
	}
}

func (ss *SourcesService) throttleAdd(source sources.DataSource, domain, sub string) {
	ss.Lock()
	defer ss.Unlock()

	ss.throttleQueue = append(ss.throttleQueue, &entry{
		Source: source,
		Domain: domain,
		Sub:    sub,
	})
}

func (ss *SourcesService) throttleNext() *entry {
	ss.Lock()
	defer ss.Unlock()

	if len(ss.throttleQueue) == 0 {
		return nil
	}

	e := ss.throttleQueue[0]
	if len(ss.throttleQueue) == 1 {
		ss.throttleQueue = []*entry{}
		return e
	}
	ss.throttleQueue = ss.throttleQueue[1:]
	return e
}

func (ss *SourcesService) processThrottleQueue() {
	max := utils.NewSemaphore(20)
	done := make(chan struct{}, 20)

	t := time.NewTicker(100 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			if !max.TryAcquire(1) {
				continue
			} else if th := ss.throttleNext(); th != nil {
				go func() {
					ss.queryOneSource(th.Source, th.Domain, th.Sub)
					done <- struct{}{}
				}()
			} else {
				max.Release(1)
			}
		case <-done:
			max.Release(1)
		case <-ss.PauseChan():
			t.Stop()
		case <-ss.ResumeChan():
			t = time.NewTicker(100 * time.Millisecond)
		case <-ss.Quit():
			return
		}
	}
}
