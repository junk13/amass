// Copyright 2017 Jeff Foley. All rights reserved.
// Use of this source code is governed by Apache 2 LICENSE that can be found in the LICENSE file.

package sources

import (
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/OWASP/Amass/amass/core"
	"github.com/OWASP/Amass/amass/utils"
)

// DNSDumpster is data source object type that implements the DataSource interface.
type DNSDumpster struct {
	BaseDataSource
}

// NewDNSDumpster returns an initialized DNSDumpster as a DataSource.
func NewDNSDumpster(srv core.AmassService) DataSource {
	d := new(DNSDumpster)

	d.BaseDataSource = *NewBaseDataSource(srv, core.SCRAPE, "DNSDumpster")
	return d
}

// Query returns the subdomain names discovered when querying this data source.
func (d *DNSDumpster) Query(domain, sub string) []string {
	var unique []string

	if domain != sub {
		return unique
	}

	u := "https://dnsdumpster.com/"
	page, err := utils.RequestWebPage(u, nil, nil, "", "")
	if err != nil {
		d.Service.Config().Log.Printf("%s: %v", u, err)
		return unique
	}

	token := d.getCSRFToken(page)
	if token == "" {
		d.Service.Config().Log.Printf("%s: Failed to obtain the CSRF token", u)
		return unique
	}
	d.Service.SetActive()

	page, err = d.postForm(token, domain)
	if err != nil {
		d.Service.Config().Log.Printf("%s: %v", u, err)
		return unique
	}
	d.Service.SetActive()

	re := utils.SubdomainRegex(domain)
	for _, sd := range re.FindAllString(page, -1) {
		if u := utils.NewUniqueElements(unique, sd); len(u) > 0 {
			unique = append(unique, u...)
		}
	}
	return unique
}

func (d *DNSDumpster) getCSRFToken(page string) string {
	re := regexp.MustCompile("<input type='hidden' name='csrfmiddlewaretoken' value='([a-zA-Z0-9]*)' />")

	if subs := re.FindStringSubmatch(page); len(subs) == 2 {
		return strings.TrimSpace(subs[1])
	}
	return ""
}

func (d *DNSDumpster) postForm(token, domain string) (string, error) {
	dial := net.Dialer{}
	client := &http.Client{
		Transport: &http.Transport{
			DialContext:         dial.DialContext,
			TLSHandshakeTimeout: 10 * time.Second,
		},
	}
	params := url.Values{
		"csrfmiddlewaretoken": {token},
		"targetip":            {domain},
	}

	req, err := http.NewRequest("POST", "https://dnsdumpster.com/", strings.NewReader(params.Encode()))
	if err != nil {
		d.Service.Config().Log.Printf("Failed to setup the POST request: %v", err)
		return "", err
	}
	// The CSRF token needs to be sent as a cookie
	cookie := &http.Cookie{
		Name:   "csrftoken",
		Domain: "dnsdumpster.com",
		Value:  token,
	}
	req.AddCookie(cookie)

	req.Header.Set("User-Agent", utils.UserAgent)
	req.Header.Set("Accept", utils.Accept)
	req.Header.Set("Accept-Language", utils.AcceptLang)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", "https://dnsdumpster.com")
	req.Header.Set("X-CSRF-Token", token)

	resp, err := client.Do(req)
	if err != nil {
		d.Service.Config().Log.Printf("The POST request failed: %v", err)
		return "", err
	}
	// Now, grab the entire page
	in, err := ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	return string(in), nil
}
