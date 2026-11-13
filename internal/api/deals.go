package api

import (
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/tomikav/steam-achievements-cli-/internal/model"
)

var storeClient = &http.Client{Timeout: 12 * time.Second}

func storeGet(path string, out any) error {
	req, _ := http.NewRequest(http.MethodGet, StoreURL+path, nil)
	req.Header.Set("User-Agent", UserAgent)
	resp, err := storeClient.Do(req)
	if err != nil {
		return fmt.Errorf("could not reach the Steam store")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Steam store answered HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// Wishlist returns the AppIDs on the user's wishlist, highest priority
// first. The wishlist must be public (it follows the profile's privacy).
func (c *Client) Wishlist() ([]int, error) {
	var out struct {
		Response struct {
			Items []struct {
				AppID    int `json:"appid"`
				Priority int `json:"priority"`
			} `json:"items"`
		} `json:"response"`
	}
	params := url.Values{"key": {c.Key}, "steamid": {c.SteamID}, "format": {"json"}}
	if err := c.get("/IWishlistService/GetWishlist/v1/", params, &out); err != nil {
		return nil, err
	}
	items := out.Response.Items
	sort.SliceStable(items, func(i, j int) bool { return items[i].Priority < items[j].Priority })
	ids := make([]int, 0, len(items))
	for _, it := range items {
		ids = append(ids, it.AppID)
	}
	return ids, nil
}

type priceOverview struct {
	Initial  string `json:"initial_formatted"`
	Final    string `json:"final_formatted"`
	Discount int    `json:"discount_percent"`
}

// Prices returns the current price of many games at once (the store allows
// batching only for prices). Games without a price (free, unreleased,
// unavailable in the region) are absent from the result.
func Prices(appIDs []int) (map[int]model.Deal, error) {
	out := map[int]model.Deal{}
	for start := 0; start < len(appIDs); start += 50 {
		chunk := appIDs[start:min(start+50, len(appIDs))]
		ids := make([]string, len(chunk))
		for i, id := range chunk {
			ids[i] = fmt.Sprint(id)
		}
		var raw map[string]struct {
			Success bool            `json:"success"`
			Data    json.RawMessage `json:"data"`
		}
		if err := storeGet("/api/appdetails?filters=price_overview&appids="+strings.Join(ids, ","), &raw); err != nil {
			return out, err
		}
		for k, v := range raw {
			var id int
			fmt.Sscan(k, &id)
			if !v.Success {
				continue
			}
			var d struct {
				Price *priceOverview `json:"price_overview"`
			}
			// Free games answer with an empty array instead of an object.
			if json.Unmarshal(v.Data, &d) != nil || d.Price == nil {
				out[id] = model.Deal{AppID: id, Free: true, Available: true, Final: "Free"}
				continue
			}
			deal := model.Deal{AppID: id, Discount: d.Price.Discount, Final: d.Price.Final, Available: true}
			if d.Price.Discount > 0 {
				deal.Original = d.Price.Initial
			}
			out[id] = deal
		}
	}
	return out, nil
}

// AppName returns a game's store name.
func AppName(appID int) (string, error) {
	var raw map[string]struct {
		Success bool `json:"success"`
		Data    struct {
			Name string `json:"name"`
		} `json:"data"`
	}
	if err := storeGet(fmt.Sprintf("/api/appdetails?filters=basic&appids=%d", appID), &raw); err != nil {
		return "", err
	}
	e := raw[fmt.Sprint(appID)]
	if !e.Success || e.Data.Name == "" {
		return "", fmt.Errorf("no store page")
	}
	return e.Data.Name, nil
}

// Specials returns the store's featured discounts.
func Specials() ([]model.Deal, error) {
	var raw struct {
		Specials struct {
			Items []struct {
				ID            int    `json:"id"`
				Name          string `json:"name"`
				Discount      int    `json:"discount_percent"`
				OriginalPrice int    `json:"original_price"`
				FinalPrice    int    `json:"final_price"`
				Currency      string `json:"currency"`
			} `json:"items"`
		} `json:"specials"`
	}
	if err := storeGet("/api/featuredcategories?l=english", &raw); err != nil {
		return nil, err
	}
	var out []model.Deal
	seen := map[int]bool{}
	for _, it := range raw.Specials.Items {
		if seen[it.ID] || it.Name == "" {
			continue
		}
		seen[it.ID] = true
		d := model.Deal{AppID: it.ID, Name: it.Name, Discount: it.Discount, Available: true,
			Final: money(it.FinalPrice, it.Currency)}
		if it.Discount > 0 {
			d.Original = money(it.OriginalPrice, it.Currency)
		}
		out = append(out, d)
	}
	return out, nil
}

func money(cents int, currency string) string {
	v := fmt.Sprintf("%d.%02d", cents/100, cents%100)
	switch currency {
	case "USD", "":
		return "$" + v
	case "EUR":
		return v + "€"
	case "GBP":
		return "£" + v
	}
	return v + " " + currency
}

// --- news ---------------------------------------------------------------------------

var (
	bbcodeRe  = regexp.MustCompile(`\[/?[a-zA-Z0-9*]+(=[^\]]*)?\]`)
	clanImgRe = regexp.MustCompile(`\{STEAM_CLAN_IMAGE\}\S*`)
	urlRe     = regexp.MustCompile(`https?://\S+`)
	htmlTagRe = regexp.MustCompile(`<[^>]+>`)
)

// News returns the latest news items of one game. It needs no API key.
func (c *Client) News(appID int, count int) ([]model.News, error) {
	var out struct {
		Appnews struct {
			Newsitems []struct {
				Title     string `json:"title"`
				URL       string `json:"url"`
				Contents  string `json:"contents"`
				Date      int64  `json:"date"`
				Feedlabel string `json:"feedlabel"`
			} `json:"newsitems"`
		} `json:"appnews"`
	}
	params := url.Values{"appid": {fmt.Sprint(appID)}, "count": {fmt.Sprint(count)}, "maxlength": {"400"}, "format": {"json"}}
	if err := c.get("/ISteamNews/GetNewsForApp/v2/", params, &out); err != nil {
		return nil, err
	}
	var items []model.News
	for _, n := range out.Appnews.Newsitems {
		items = append(items, model.News{AppID: appID, Title: strings.TrimSpace(n.Title), URL: n.URL, Date: n.Date,
			Summary: cleanNews(n.Contents), Feed: n.Feedlabel})
	}
	return items, nil
}

func cleanNews(s string) string {
	s = clanImgRe.ReplaceAllString(s, " ")
	s = bbcodeRe.ReplaceAllString(s, " ")
	s = htmlTagRe.ReplaceAllString(s, " ")
	s = urlRe.ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	return strings.Join(strings.Fields(s), " ")
}
