package api

// The store views: featured lists, search and the detail of one game. All
// of it is public data from store.steampowered.com and needs no API key.
// Buying always happens inside Steam; this code never touches payments.

import (
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"strings"

	"github.com/tomikav/steam-achievements-cli-/internal/model"
)

// Store sections, in display order.
const (
	SectionSpecials = "specials"
	SectionTop      = "top_sellers"
	SectionNew      = "new_releases"
	SectionSoon     = "coming_soon"
)

type featuredItem struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Discount int    `json:"discount_percent"`
	Original *int   `json:"original_price"`
	Final    int    `json:"final_price"`
	Currency string `json:"currency"`
	Image    string `json:"large_capsule_image"`
	Header   string `json:"header_image"`
	Windows  bool   `json:"windows_available"`
	Mac      bool   `json:"mac_available"`
	Linux    bool   `json:"linux_available"`
}

// StoreFront returns the store's featured lists by section.
func StoreFront() (map[string][]model.StoreItem, error) {
	var raw map[string]json.RawMessage
	if err := storeGet("/api/featuredcategories?l=english", &raw); err != nil {
		return nil, err
	}
	out := map[string][]model.StoreItem{}
	for _, sec := range []string{SectionSpecials, SectionTop, SectionNew, SectionSoon} {
		var s struct {
			Items []featuredItem `json:"items"`
		}
		if json.Unmarshal(raw[sec], &s) != nil {
			continue
		}
		seen := map[int]bool{}
		for _, it := range s.Items {
			if seen[it.ID] || it.Name == "" || it.ID == 0 {
				continue
			}
			seen[it.ID] = true
			out[sec] = append(out[sec], it.toItem(sec == SectionSoon))
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("the Steam store returned no lists")
	}
	return out, nil
}

func (it featuredItem) toItem(soon bool) model.StoreItem {
	s := model.StoreItem{AppID: it.ID, Name: it.Name, Discount: it.Discount, Cents: it.Final, Currency: it.Currency,
		Image: it.Header, Soon: soon, Platforms: platforms(it.Windows, it.Mac, it.Linux)}
	if s.Image == "" {
		s.Image = it.Image
	}
	switch {
	case it.Final == 0 && !soon:
		s.Free, s.Final = true, "Free"
	case it.Final > 0:
		s.Final = money(it.Final, it.Currency)
	}
	if it.Discount > 0 && it.Original != nil {
		s.Original = money(*it.Original, it.Currency)
	}
	return s
}

func platforms(w, m, l bool) string {
	s := ""
	if w {
		s += "w"
	}
	if m {
		s += "m"
	}
	if l {
		s += "l"
	}
	return s
}

// StoreSearch finds games in the store by name.
func StoreSearch(term string) ([]model.StoreItem, error) {
	var raw struct {
		Items []struct {
			Type  string `json:"type"`
			ID    int    `json:"id"`
			Name  string `json:"name"`
			Price *struct {
				Currency string `json:"currency"`
				Initial  int    `json:"initial"`
				Final    int    `json:"final"`
			} `json:"price"`
			Image     string          `json:"tiny_image"`
			Metascore string          `json:"metascore"`
			Platforms map[string]bool `json:"platforms"`
		} `json:"items"`
	}
	if err := storeGet("/api/storesearch/?l=english&term="+url.QueryEscape(term), &raw); err != nil {
		return nil, err
	}
	var out []model.StoreItem
	for _, it := range raw.Items {
		if it.Type != "" && it.Type != "app" {
			continue
		}
		s := model.StoreItem{AppID: it.ID, Name: html.UnescapeString(it.Name), Image: it.Image,
			Platforms: platforms(it.Platforms["windows"], it.Platforms["mac"], it.Platforms["linux"])}
		fmt.Sscan(it.Metascore, &s.Metascore)
		if it.Price == nil {
			s.Free, s.Final = true, "Free"
		} else {
			s.Cents, s.Currency, s.Final = it.Price.Final, it.Price.Currency, money(it.Price.Final, it.Price.Currency)
			if it.Price.Initial > it.Price.Final && it.Price.Initial > 0 {
				s.Original = money(it.Price.Initial, it.Price.Currency)
				s.Discount = int(float64(it.Price.Initial-it.Price.Final)/float64(it.Price.Initial)*100 + 0.5)
			}
		}
		out = append(out, s)
	}
	return out, nil
}

// cleanText turns store HTML into one line of plain text.
func cleanText(s string) string {
	return strings.Join(strings.Fields(html.UnescapeString(tagRe.ReplaceAllString(s, " "))), " ")
}
