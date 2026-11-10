package api

import (
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"strings"
	"time"

	"github.com/tomikav/steam-achievements-cli-/internal/model"
)

// StoreURL is the Steam store root (a variable so tests can replace it).
var StoreURL = "https://store.steampowered.com"

// StoreDetails fetches a game's public store data. It needs no API key.
func StoreDetails(appID int) (model.StoreInfo, error) {
	cl := &http.Client{Timeout: 10 * time.Second}
	req, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/appdetails?appids=%d&l=english", StoreURL, appID), nil)
	req.Header.Set("User-Agent", UserAgent)
	resp, err := cl.Do(req)
	if err != nil {
		return model.StoreInfo{}, fmt.Errorf("could not reach the Steam store")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return model.StoreInfo{}, fmt.Errorf("Steam store answered HTTP %d", resp.StatusCode)
	}
	var raw map[string]struct {
		Success bool `json:"success"`
		Data    struct {
			Name        string   `json:"name"`
			Short       string   `json:"short_description"`
			Developers  []string `json:"developers"`
			Publishers  []string `json:"publishers"`
			HeaderImage string   `json:"header_image"`
			Genres      []struct {
				Description string `json:"description"`
			} `json:"genres"`
			Release struct {
				Date string `json:"date"`
			} `json:"release_date"`
			Metacritic struct {
				Score int `json:"score"`
			} `json:"metacritic"`
			Platforms map[string]bool `json:"platforms"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return model.StoreInfo{}, fmt.Errorf("unexpected answer from the Steam store")
	}
	entry, ok := raw[fmt.Sprint(appID)]
	if !ok || !entry.Success {
		return model.StoreInfo{}, fmt.Errorf("no store page for this game")
	}
	d := entry.Data
	info := model.StoreInfo{
		Name:        d.Name,
		Description: strings.Join(strings.Fields(html.UnescapeString(tagRe.ReplaceAllString(d.Short, " "))), " "),
		Developers:  d.Developers,
		Publishers:  d.Publishers,
		Released:    d.Release.Date,
		Metacritic:  d.Metacritic.Score,
		HeaderImage: d.HeaderImage,
	}
	for _, g := range d.Genres {
		info.Genres = append(info.Genres, g.Description)
	}
	for _, p := range []string{"windows", "mac", "linux"} {
		if d.Platforms[p] {
			info.Platforms = append(info.Platforms, p)
		}
	}
	return info, nil
}
