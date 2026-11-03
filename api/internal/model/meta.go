package model

import "net/http"

// ParseMetaSearch returns the optional search parameter of the meta list
// endpoints: a case-insensitive substring of the listed values, "" for the
// default list.
func ParseMetaSearch(r *http.Request) (string, error) {
	p := newQueryParams(r)
	search := p.str("search")
	if err := p.err(); err != nil {
		return "", err
	}
	return search, nil
}
