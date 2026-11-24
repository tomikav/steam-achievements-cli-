package ui

// Store view (`s`): the Steam store from the terminal. Featured lists,
// search, a detail pane with the price and what the game is, and a local
// cart that remembers the price each game had when you added it. Paying is
// always done inside Steam: `b` opens the game's page in the Steam client.

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/tomikav/steam-achievements-cli-/internal/icons"
	"github.com/tomikav/steam-achievements-cli-/internal/model"
)

var storeTabs = []string{"SPECIALS", "TOP SELLERS", "NEW", "COMING SOON", "SEARCH", "CART"}

var storeSections = []string{"specials", "top_sellers", "new_releases", "coming_soon"}

const (
	tabSearch = 4
	tabCart   = 5
)

type storeState struct {
	tab            int
	lists          map[string][]model.StoreItem
	loading        bool
	loaded         bool
	err            error
	query          string
	typing         bool
	searching      bool
	searchErr      error
	results        []model.StoreItem
	cursor, offset [6]int
	details        map[int]*model.StoreInfo
	detailErr      map[int]error
	detailBusy     int
	focus          bool // detail shown full width
	cart           []model.CartItem
	cartLoaded     bool
	back           browseMode
	wantFront      bool
	wantSearch     string
}

func (b *browser) openStore() {
	st := &b.store
	if b.mode != modeStore {
		st.back = b.mode
		if st.back != modeHome {
			st.back = modeList
		}
	}
	b.mode, b.full = modeStore, true
	if st.details == nil {
		st.details, st.detailErr = map[int]*model.StoreInfo{}, map[int]error{}
	}
	if !st.cartLoaded {
		st.cartLoaded = true
		if b.opt.LoadCart != nil {
			st.cart = b.opt.LoadCart()
		}
	}
	if !st.loaded && !st.loading {
		if b.opt.LoadStoreFront == nil {
			st.err = fmt.Errorf("the store is not available here")
			return
		}
		st.loading, st.wantFront = true, true
	}
}

// storeSearch switches to the search tab and runs a query.
func (b *browser) storeSearch(q string) {
	b.openStore()
	st := &b.store
	st.tab, st.query, st.typing = tabSearch, q, false
	if strings.TrimSpace(q) != "" {
		st.searching, st.searchErr, st.wantSearch = true, nil, q
	}
}

func (b *browser) storeItems() []model.StoreItem {
	st := &b.store
	switch st.tab {
	case tabSearch:
		return st.results
	case tabCart:
		out := make([]model.StoreItem, len(st.cart))
		for i, c := range st.cart {
			out[i] = c.StoreItem
		}
		return out
	}
	return st.lists[storeSections[st.tab]]
}

func (b *browser) storeSelected() (model.StoreItem, bool) {
	items := b.storeItems()
	c := b.store.cursor[b.store.tab]
	if c < 0 || c >= len(items) {
		return model.StoreItem{}, false
	}
	return items[c], true
}

func (b *browser) inCart(id int) int {
	for i, c := range b.store.cart {
		if c.AppID == id {
			return i
		}
	}
	return -1
}

func (b *browser) owned(id int) bool {
	_, ok := b.gameByID(id)
	return ok
}

func (b *browser) storePerPage() int { return max(1, (b.h-7)/entryH) }

func (b *browser) storeKey(k string) {
	st := &b.store
	if st.typing {
		switch k {
		case "enter":
			b.storeSearch(st.query)
		case "esc":
			st.typing = false
		case "backspace":
			if r := []rune(st.query); len(r) > 0 {
				st.query = string(r[:len(r)-1])
			}
		default:
			if k == "space" {
				k = " "
			}
			if len([]rune(k)) == 1 {
				st.query += k
			}
		}
		return
	}
	items := b.storeItems()
	cur := &st.cursor[st.tab]
	off := &st.offset[st.tab]
	per := b.storePerPage()
	switch k {
	case "esc", "backspace":
		if st.focus {
			st.focus, b.full = false, true
			return
		}
		b.mode, b.full = st.back, true
		return
	case "down", "j", "wheeldown":
		*cur++
	case "up", "k", "wheelup":
		*cur--
	case "pgdown":
		*cur += per
	case "pgup":
		*cur -= per
	case "home", "g":
		*cur = 0
	case "end", "G":
		*cur = len(items) - 1
	case "right", "tab", "l":
		st.tab, st.focus, b.full = (st.tab+1)%len(storeTabs), false, true
		return
	case "left", "shift+tab", "h":
		st.tab, st.focus, b.full = (st.tab+len(storeTabs)-1)%len(storeTabs), false, true
		return
	case "1", "2", "3", "4", "5", "6":
		st.tab, st.focus, b.full = int(k[0]-'1'), false, true
		return
	case "/", "s":
		st.tab, st.typing, st.focus, b.full = tabSearch, true, false, true
		return
	case "R":
		st.loaded, st.err = false, nil
		b.openStore()
		return
	}
	it, ok := b.storeSelected()
	if !ok {
		clampScroll(cur, off, len(items), per)
		return
	}
	switch k {
	case "enter":
		if b.owned(it.AppID) {
			b.openPlay(it.AppID)
			return
		}
		st.focus, b.full = !st.focus, true
	case "c", "x":
		if i := b.inCart(it.AppID); i >= 0 {
			st.cart = append(st.cart[:i], st.cart[i+1:]...)
			b.status = "removed from your cart: " + it.Name
		} else if k == "c" {
			if b.owned(it.AppID) {
				b.status = it.Name + " is already in your library"
				return
			}
			st.cart = append(st.cart, model.CartItem{StoreItem: it, Added: time.Now().Unix(), AddedCents: it.Cents})
			b.status = "added to your cart: " + it.Name
		}
		if b.opt.SaveCart != nil {
			b.opt.SaveCart(st.cart)
		}
	case "b":
		switch {
		case b.owned(it.AppID):
			b.status = "you already own " + it.Name + "  (enter opens it)"
		case b.opt.BuyInSteam == nil:
			b.status = "demo mode: this would open " + it.Name + " in Steam to buy it"
		case b.opt.BuyInSteam(it.AppID) != nil:
			b.status = "could not open Steam"
		default:
			b.status = "opened " + it.Name + " in Steam. Checkout happens there."
		}
	case "i":
		switch {
		case !it.Free && !b.owned(it.AppID):
			b.status = it.Name + " is not free: b opens it in Steam to buy"
		case b.opt.InstallApp == nil:
			b.status = "demo mode: this would install " + it.Name
		case b.opt.InstallApp(it.AppID) != nil:
			b.status = "could not ask Steam to install it"
		default:
			b.status = "Steam is installing " + it.Name
		}
	case "o":
		if b.opt.Demo {
			b.status = "demo mode: nothing to open"
		} else if openTarget(fmt.Sprintf("https://store.steampowered.com/app/%d", it.AppID)) == nil {
			b.status = "opened in your browser"
		}
	}
	clampScroll(cur, off, len(items), per)
}

// storeIO runs the store loads.
func (b *browser) storeIO(events chan<- func()) {
	st := &b.store
	opt := b.opt
	if st.wantFront && opt.LoadStoreFront != nil {
		st.wantFront = false
		go func() {
			lists, err := opt.LoadStoreFront()
			events <- func() { st.loading, st.loaded, st.err, st.lists = false, err == nil, err, lists }
		}()
	}
	if q := st.wantSearch; q != "" && opt.SearchStore != nil {
		st.wantSearch = ""
		go func() {
			res, err := opt.SearchStore(q)
			events <- func() {
				if st.query == q {
					st.searching, st.searchErr, st.results = false, err, res
					st.cursor[tabSearch], st.offset[tabSearch] = 0, 0
				}
			}
		}()
	}
	// details of the selected game, one at a time
	if b.mode == modeStore && st.detailBusy == 0 && opt.LoadStore != nil {
		if it, ok := b.storeSelected(); ok {
			id := it.AppID
			if st.details[id] == nil && st.detailErr[id] == nil {
				st.detailBusy = id
				go func() {
					info, err := opt.LoadStore(id)
					events <- func() {
						st.detailBusy = 0
						if err != nil {
							st.detailErr[id] = err
						} else {
							st.details[id] = &info
						}
					}
				}()
			}
		}
	}
}

func priceBlock(it model.StoreItem) string {
	switch {
	case it.Soon && it.Final == "":
		return paint(active.Dim, "coming soon")
	case it.Free:
		return bold(active.Good, "FREE")
	case it.Discount > 0:
		return chip(active.Good, active.Ink, fmt.Sprintf("-%d%%", it.Discount)) + " " +
			strike(paint(active.Dim, it.Original)) + " " + bold(active.Good, it.Final)
	case it.Final != "":
		return bold(active.Text, it.Final)
	}
	return paint(active.Dim, "no price")
}

// strike draws a crossed-out price (SGR 9).
func strike(s string) string {
	if s == "" {
		return ""
	}
	return "\x1b[9m" + s + "\x1b[29m"
}

func (b *browser) cartTotal() (total, saved int, currency string) {
	for _, c := range b.store.cart {
		total += c.Cents
		if c.AddedCents > c.Cents {
			saved += c.AddedCents - c.Cents
		}
		if c.Currency != "" {
			currency = c.Currency
		}
	}
	return
}

func (b *browser) buildStore(f *frame) {
	st := &b.store
	right := ""
	if n := len(st.cart); n > 0 {
		total, _, cur := b.cartTotal()
		right = paint(active.Dim, "cart ") + bold(active.Text, fmt.Sprint(n)) + paint(active.Dim, "  ·  ") + bold(active.Good, moneyFmt(total, cur)) + " "
	}
	b.header(f, right)
	line := " " + chip(active.Accent, active.Ink, "STORE") + "   "
	for i, t := range storeTabs {
		label := t
		if i == tabCart && len(st.cart) > 0 {
			label = fmt.Sprintf("%s %d", t, len(st.cart))
		}
		if i == st.tab {
			line += bold(active.To, label)
		} else {
			line += paint(active.Dim, label)
		}
		line += "    "
	}
	f.put(1, 0, padRight(truncateANSI(line, b.w), b.w))

	// search bar / hint
	var hint string
	switch {
	case st.typing:
		hint = "  " + bold(active.Accent, "search the store › ") + paint(active.Text, st.query) + paint(pulse(active.Accent, active.Faint), "█") +
			paint(active.Dim, "   enter to search, esc to cancel")
	case st.tab == tabSearch && st.query != "":
		hint = "  " + paint(active.Dim, "results for ") + bold(active.Text, st.query) + paint(active.Dim, "   / new search")
	case st.tab == tabCart:
		total, saved, cur := b.cartTotal()
		hint = "  " + paint(active.Dim, "a wish list that lives on your PC. Total ") + bold(active.Good, moneyFmt(total, cur))
		if saved > 0 {
			hint += paint(active.Dim, ", ") + bold(active.Good, moneyFmt(saved, cur)) + paint(active.Dim, " cheaper than when you added them")
		}
	default:
		hint = "  " + paint(active.Dim, "/ search    c add to cart    b buy in Steam    i install free games    enter details")
	}
	f.put(2, 0, padRight(truncateANSI(hint, b.w), b.w))
	f.put(3, 0, paint(active.Faint, strings.Repeat("─", b.w)))

	h := b.h - 5
	for r := 0; r < h; r++ {
		f.put(4+r, 0, strings.Repeat(" ", b.w))
	}
	center := func(msg string, x, w int) {
		f.put(4+h/2, x+max((w-width(msg))/2, 0), msg)
	}
	items := b.storeItems()
	switch {
	case st.err != nil && st.tab < tabSearch:
		center(chip(active.Bad, active.Ink, "ERROR")+" "+paint(active.Text, st.err.Error())+paint(active.Dim, "   R to retry"), 0, b.w)
		return
	case st.loading && st.tab < tabSearch:
		center(paint(active.Accent, b.spin())+" "+paint(active.Text, "Opening the store..."), 0, b.w)
		return
	case st.tab == tabSearch && st.searching:
		center(paint(active.Accent, b.spin())+" "+paint(active.Text, "Searching for "+st.query+"..."), 0, b.w)
		return
	case st.tab == tabSearch && st.searchErr != nil:
		center(chip(active.Bad, active.Ink, "ERROR")+" "+paint(active.Text, st.searchErr.Error()), 0, b.w)
		return
	case len(items) == 0:
		msg := "Nothing here."
		switch st.tab {
		case tabSearch:
			msg = "Press / and type a game name."
			if st.query != "" {
				msg = "No games found for " + st.query + "."
			}
		case tabCart:
			msg = "Your cart is empty. Press c on any game to keep it here."
		}
		center(paint(active.Dim, msg), 0, b.w)
		return
	}

	it, _ := b.storeSelected()
	listW := b.w
	paneX := 0
	if st.focus {
		b.buildStoreDetail(f, it, 2, 4, b.w-4, h)
		return
	}
	if b.w >= 120 {
		listW = b.w * 52 / 100
		paneX = listW + 2
		for r := 0; r < h; r++ {
			f.put(4+r, listW, paint(active.Faint, "│"))
		}
		b.buildStoreDetail(f, it, paneX+1, 4, b.w-paneX-3, h)
	}

	per := b.storePerPage()
	cur, off := &st.cursor[st.tab], &st.offset[st.tab]
	clampScroll(cur, off, len(items), per)
	for i := 0; i < per; i++ {
		idx := *off + i
		if idx >= len(items) {
			break
		}
		x := items[idx]
		top := 4 + i*entryH
		sel := idx == *cur
		marker := "   "
		if sel {
			marker = " " + paint(pulse(active.Accent, active.To), "▌") + " "
		}
		price := priceBlock(x)
		nameW := max(listW-width(price)-10, 8)
		name := paint(active.Text, Truncate(x.Name, nameW))
		if sel {
			name = bold(active.To, Truncate(x.Name, nameW))
		}
		f.put(top, 0, marker)
		f.put(top, 4, name)
		f.put(top, max(listW-width(price)-2, 4+width(name)+1), price)
		var tags []string
		if b.owned(x.AppID) {
			tags = append(tags, chip(active.From, active.Ink, "IN LIBRARY"))
		}
		if ci := b.inCart(x.AppID); ci >= 0 {
			tags = append(tags, chip(active.Accent, active.Ink, "IN CART"))
			c := b.store.cart[ci]
			if c.AddedCents > c.Cents && st.tab == tabCart {
				tags = append(tags, paint(active.Good, "price dropped "+moneyFmt(c.AddedCents-c.Cents, c.Currency)))
			}
		}
		if p := platformText(x.Platforms); p != "" {
			tags = append(tags, paint(active.Dim, p))
		}
		if x.Metascore > 0 {
			tags = append(tags, paint(active.Dim, fmt.Sprintf("metascore %d", x.Metascore)))
		}
		f.put(top+1, 4, truncateANSI(strings.Join(tags, "  "), listW-6))
	}
}

func platformText(p string) string {
	var out []string
	for _, c := range p {
		switch c {
		case 'w':
			out = append(out, "Windows")
		case 'm':
			out = append(out, "macOS")
		case 'l':
			out = append(out, "Linux")
		}
	}
	return strings.Join(out, " · ")
}

func moneyFmt(cents int, currency string) string {
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

// buildStoreDetail draws the selected game: art, price, what it is.
func (b *browser) buildStoreDetail(f *frame, it model.StoreItem, x, top, w, h int) {
	st := &b.store
	row := top
	info := st.details[it.AppID]
	img := it.Image
	if info != nil && info.HeaderImage != "" {
		img = info.HeaderImage
	}
	if b.icons && img != "" {
		cols := min(w, 60)
		pxW := cols * b.cellW
		pxH := pxW * 215 / 460
		rows := int(math.Ceil(float64(pxH) / float64(b.cellH)))
		if rows > h/3 {
			rows = h / 3
			pxH = rows * b.cellH
			pxW = pxH * 460 / 215
			cols = int(math.Ceil(float64(pxW) / float64(b.cellW)))
		}
		key := fmt.Sprintf("shdr%d@%dx%d", it.AppID, pxW, pxH)
		if s, ok := b.big[key]; ok {
			if s != "" {
				f.icons = append(f.icons, iconSpot{row: row, col: x, w: cols, h: rows, key: key, sixel: s})
				row += rows + 1
			}
		} else {
			b.wantBig = &icons.Request{Key: key, URL: img, W: pxW, H: pxH}
			row += rows + 1
		}
	}
	put := func(s string) {
		if row < top+h {
			f.put(row, x, truncateANSI(s, w))
			row++
		}
	}
	put(bold(active.Text, Truncate(it.Name, w)))
	price := priceBlock(it)
	if info != nil && info.Price != "" && it.Final == "" {
		price = bold(active.Text, info.Price)
	}
	put(price)
	put("")
	if info == nil {
		if err := st.detailErr[it.AppID]; err != nil {
			put(paint(active.Dim, err.Error()))
		} else {
			put(paint(active.Accent, b.spin()) + paint(active.Dim, " loading details..."))
		}
	} else {
		for _, l := range wrap(info.Description, w, 4) {
			put(paint(active.Text, l))
		}
		put("")
		kv := func(k, v string) {
			if v != "" {
				put(paint(active.Dim, padRight(k, 13)) + paint(active.Text, Truncate(v, w-13)))
			}
		}
		kv("genre", strings.Join(info.Genres, ", "))
		kv("developer", strings.Join(info.Developers, ", "))
		kv("released", info.Released)
		if info.Reviews > 0 {
			kv("reviews", fmt.Sprintf("%s recommendations", thousands(info.Reviews)))
		}
		if info.Metacritic > 0 {
			kv("metacritic", fmt.Sprint(info.Metacritic))
		}
		if info.Achievements > 0 {
			kv("achievements", fmt.Sprint(info.Achievements))
		}
		kv("platforms", strings.Join(info.Platforms, ", "))
		if info.ControllerSupp != "" {
			kv("controller", info.ControllerSupp+" support")
		}
	}
	put("")
	var keys []string
	switch {
	case b.owned(it.AppID):
		keys = append(keys, chip(active.From, active.Ink, "IN YOUR LIBRARY"), bold(active.Accent, "enter")+paint(active.Dim, " open"))
	case it.Free:
		keys = append(keys, bold(active.Accent, "i")+paint(active.Dim, " install free"))
	default:
		keys = append(keys, bold(active.Accent, "b")+paint(active.Dim, " buy in Steam"))
	}
	if b.inCart(it.AppID) >= 0 {
		keys = append(keys, bold(active.Accent, "x")+paint(active.Dim, " remove from cart"))
	} else if !b.owned(it.AppID) {
		keys = append(keys, bold(active.Accent, "c")+paint(active.Dim, " add to cart"))
	}
	keys = append(keys, bold(active.Accent, "o")+paint(active.Dim, " web"))
	put(strings.Join(keys, "   "))
}

func thousands(n int) string {
	s := fmt.Sprint(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}
