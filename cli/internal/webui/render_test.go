package webui

import (
	"reflect"
	"testing"
)

// Every page's data embeds Base, which the shared layout reads (.Title, .Active, ...).
// A page field with one of those names silently shadows it and breaks the layout at
// render time (QueuePageData once had an int "Active" and the queue page returned 500).
func TestPageDataDoesNotShadowBase(t *testing.T) {
	base := map[string]bool{}
	bt := reflect.TypeOf(Base{})
	for i := 0; i < bt.NumField(); i++ {
		base[bt.Field(i).Name] = true
	}
	pages := []any{
		OverviewPageData{}, SearchPageData{}, UsersPageData{}, UserPageData{}, UsersImportPageData{},
		GroupsPageData{}, GroupPageData{}, AliasesPageData{}, DomainsPageData{}, DomainPageData{},
		QueuePageData{}, BansPageData{}, ServicesPageData{}, LogsPageData{}, AuditPageData{},
		AdminsPageData{}, SettingsPageData{}, TokensPageData{},
	}
	for _, p := range pages {
		pt := reflect.TypeOf(p)
		for i := 0; i < pt.NumField(); i++ {
			f := pt.Field(i)
			if !f.Anonymous && base[f.Name] {
				t.Errorf("%s.%s shadows Base.%s", pt.Name(), f.Name, f.Name)
			}
		}
	}
}
