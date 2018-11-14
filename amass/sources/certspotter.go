// Copyright 2017 Jeff Foley. All rights reserved.
// Use of this source code is governed by Apache 2 LICENSE that can be found in the LICENSE file.

package sources

import (
	"fmt"

	"github.com/OWASP/Amass/amass/core"
	"github.com/OWASP/Amass/amass/utils"
)

// CertSpotter is data source object type that implements the DataSource interface.
type CertSpotter struct {
	BaseDataSource
}

// NewCertSpotter returns an initialized CertSpotter as a DataSource.
func NewCertSpotter(srv core.AmassService) DataSource {
	c := new(CertSpotter)

	c.BaseDataSource = *NewBaseDataSource(srv, core.CERT, "CertSpotter")
	return c
}

// Query returns the subdomain names discovered when querying this data source.
func (c *CertSpotter) Query(domain, sub string) []string {
	var unique []string

	if domain != sub {
		return unique
	}

	url := c.getURL(domain)
	page, err := utils.RequestWebPage(url, nil, nil, "", "")
	if err != nil {
		c.Service.Config().Log.Printf("%s: %v", url, err)
		return unique
	}

	c.Service.SetActive()
	re := utils.SubdomainRegex(domain)
	for _, sd := range re.FindAllString(page, -1) {
		if u := utils.NewUniqueElements(unique, sd); len(u) > 0 {
			unique = append(unique, u...)
		}
	}
	return unique
}

func (c *CertSpotter) getURL(domain string) string {
	format := "https://certspotter.com/api/v0/certs?domain=%s"

	return fmt.Sprintf(format, domain)
}
