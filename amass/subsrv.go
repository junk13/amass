// Copyright 2017 Jeff Foley. All rights reserved.
// Use of this source code is governed by Apache 2 LICENSE that can be found in the LICENSE file.

package amass

import (
	"strings"
	"time"

	"github.com/OWASP/Amass/amass/core"
	"github.com/OWASP/Amass/amass/utils"
	evbus "github.com/asaskevich/EventBus"
)

// SubdomainService is the AmassService that handles all newly discovered names
// within the architecture. This is achieved by receiving all the RESOLVED events.
type SubdomainService struct {
	core.BaseAmassService

	bus evbus.Bus

	// Ensures we do not completely process names more than once
	filter *utils.StringFilter

	// Subdomain names that have been seen and how many times
	subdomains map[string]int

	releases chan struct{}

	completions chan time.Time
}

// NewSubdomainService requires the enumeration configuration and event bus as parameters.
// The object returned is initialized, but has not yet been started.
func NewSubdomainService(config *core.AmassConfig, bus evbus.Bus) *SubdomainService {
	max := core.TimingToMaxFlow(config.Timing) + core.TimingToReleasesPerSecond(config.Timing)
	ss := &SubdomainService{
		bus:         bus,
		filter:      utils.NewStringFilter(),
		subdomains:  make(map[string]int),
		releases:    make(chan struct{}, max),
		completions: make(chan time.Time, max),
	}

	ss.BaseAmassService = *core.NewBaseAmassService("Subdomain Service", config, ss)
	return ss
}

// OnStart implements the AmassService interface
func (ss *SubdomainService) OnStart() error {
	ss.BaseAmassService.OnStart()

	ss.bus.SubscribeAsync(core.NEWNAME, ss.SendRequest, false)
	ss.bus.SubscribeAsync(core.RESOLVED, ss.performCheck, false)
	ss.bus.SubscribeAsync(core.RELEASEREQ, ss.sendRelease, false)
	go ss.processRequests()
	go ss.processReleases()
	return nil
}

// OnPause implements the AmassService interface
func (ss *SubdomainService) OnPause() error {
	return nil
}

// OnResume implements the AmassService interface
func (ss *SubdomainService) OnResume() error {
	return nil
}

// OnStop implements the AmassService interface
func (ss *SubdomainService) OnStop() error {
	ss.BaseAmassService.OnStop()

	ss.bus.Unsubscribe(core.NEWNAME, ss.SendRequest)
	ss.bus.Unsubscribe(core.CHECKED, ss.performCheck)
	ss.bus.Unsubscribe(core.RELEASEREQ, ss.sendRelease)
	return nil
}

func (ss *SubdomainService) processRequests() {
	var perSec []int
	var completionTimes []time.Time

	t := time.NewTicker(time.Second)
	defer t.Stop()
	logTick := time.NewTicker(time.Minute)
	defer logTick.Stop()
	for {
		select {
		case <-ss.PauseChan():
			<-ss.ResumeChan()
		case <-ss.Quit():
			return
		case comp := <-ss.completions:
			completionTimes = append(completionTimes, comp)
		case <-t.C:
			perSec = append(perSec, len(completionTimes))
			completionTimes = []time.Time{}
		case <-logTick.C:
			num := len(perSec)
			var total int
			for _, s := range perSec {
				total += s
			}
			ss.Config().Log.Printf("Average requests processed: %d per second", total/num)
			perSec = []int{}
		case req := <-ss.RequestChan():
			go ss.performRequest(req)
		}
	}
}

func (ss *SubdomainService) sendCompletionTime(t time.Time) {
	ss.completions <- t
}

func (ss *SubdomainService) performRequest(req *core.AmassRequest) {
	if req == nil || req.Name == "" || req.Domain == "" {
		ss.sendRelease()
		return
	}

	ss.SetActive()
	go ss.sendCompletionTime(time.Now())
	req.Name = strings.ToLower(utils.RemoveAsteriskLabel(req.Name))
	req.Domain = strings.ToLower(req.Domain)
	if ss.Config().Passive {
		if !ss.filter.Duplicate(req.Name) {
			ss.bus.Publish(core.OUTPUT, &core.AmassOutput{
				Name:   req.Name,
				Domain: req.Domain,
				Tag:    req.Tag,
				Source: req.Source,
			})
		}
		ss.sendRelease()
		return
	}
	ss.bus.Publish(core.DNSQUERY, req)
}

func (ss *SubdomainService) performCheck(req *core.AmassRequest) {
	ss.SetActive()

	if ss.Config().IsDomainInScope(req.Name) {
		ss.checkSubdomain(req)
	}
	if req.Tag == core.DNS {
		ss.sendCompletionTime(time.Now())
	}
	ss.bus.Publish(core.CHECKED, req)
}

func (ss *SubdomainService) checkSubdomain(req *core.AmassRequest) {
	labels := strings.Split(req.Name, ".")
	num := len(labels)
	// Is this large enough to consider further?
	if num < 2 {
		return
	}
	// It cannot have fewer labels than the root domain name
	if num-1 < len(strings.Split(req.Domain, ".")) {
		return
	}
	// Do not further evaluate service subdomains
	if labels[1] == "_tcp" || labels[1] == "_udp" || labels[1] == "_tls" {
		return
	}
	// CNAMEs are not a proper subdomain
	sub := strings.Join(labels[1:], ".")
	if ss.Config().Graph().CNAMENode(sub) != nil {
		return
	}

	ss.bus.Publish(core.NEWSUB, &core.AmassRequest{
		Name:   sub,
		Domain: req.Domain,
		Tag:    req.Tag,
		Source: req.Source,
	}, ss.timesForSubdomain(sub))
}

func (ss *SubdomainService) timesForSubdomain(sub string) int {
	ss.Lock()
	defer ss.Unlock()

	times, ok := ss.subdomains[sub]
	if ok {
		times++
	} else {
		times = 1
	}
	ss.subdomains[sub] = times
	return times
}

func (ss *SubdomainService) sendRelease() {
	ss.releases <- struct{}{}
}

func (ss *SubdomainService) processReleases() {
	t := time.NewTicker(core.TimingToReleaseDelay(ss.Config().Timing))
	defer t.Stop()

	var rcount int
	for {
		select {
		case <-ss.Quit():
			return
		case <-t.C:
			if rcount > 0 {
				ss.Config().MaxFlow.Release(1)
				rcount--
			}
		case <-ss.releases:
			rcount++
		}
	}
}
