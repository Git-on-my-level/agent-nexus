package app

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
)

var (
	rulingDateID       = regexp.MustCompile(`^R-[0-9]{8}-[0-9]{2}$`)
	rulingAliasPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 _.-]{0,95}$`)
	rulingTopicPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
)

func validRulingDateID(id string) bool {
	if !rulingDateID.MatchString(id) || id[11:] == "00" {
		return false
	}
	_, err := time.Parse("20060102", id[2:10])
	return err == nil
}

type rulingProposalArgs struct {
	date, scope, topic, title, bodyFile, quote string
	aliases, relations                         []string
}

func (p *rulingProposalArgs) bind(fs *flag.FlagSet) {
	fs.StringVar(&p.date, "id-date", "", "Pinned ID or decision date")
	fs.StringVar(&p.scope, "scope", "", "workspace or account")
	fs.StringVar(&p.topic, "topic", "", "Ruling topic")
	fs.StringVar(&p.title, "title", "", "Display title; defaults to the first body line")
	fs.StringVar(&p.bodyFile, "body-file", "", "Markdown file or - for stdin")
	fs.StringVar(&p.quote, "quote", "", "Direct quote from the proposed decision")
	fs.Func("alias", "Legacy name (repeatable)", func(value string) error {
		p.aliases = append(p.aliases, value)
		return nil
	})
	fs.Func("relation", "kind:R-YYYYMMDD-NN (repeatable)", func(value string) error {
		p.relations = append(p.relations, value)
		return nil
	})
}

func (p rulingProposalArgs) validate() error {
	if _, err := time.Parse(time.DateOnly, p.date); err != nil && !validRulingDateID(p.date) {
		return errnorm.Usage("invalid_ruling_id", "--id-date requires YYYY-MM-DD or R-YYYYMMDD-NN")
	}
	if (p.scope != "workspace" && p.scope != "account") || !rulingTopicPattern.MatchString(p.topic) || strings.TrimSpace(p.quote) == "" || p.bodyFile == "" {
		return errnorm.Usage("invalid_ruling_proposal", "--scope account|workspace, --topic, --quote and --body-file are required")
	}
	seen := map[string]bool{p.date: true}
	for _, raw := range p.relations {
		kind, id, _ := strings.Cut(raw, ":")
		if !validRulingDateID(id) || seen[id] || (kind != "supersedes" && kind != "amends" && kind != "independent") {
			return errnorm.Usage("invalid_ruling_relation", "use one --relation supersedes|amends|independent:R-YYYYMMDD-NN per target")
		}
		seen[id] = true
	}
	if err := validateRulingAliases(p.aliases); err != nil {
		return err
	}
	return nil
}

func validateRulingAliases(aliases []string) error {
	seen := map[string]bool{}
	for _, alias := range aliases {
		if !rulingAliasPattern.MatchString(alias) || strings.TrimSpace(alias) != alias || rulingDateID.MatchString(alias) || seen[alias] {
			return errnorm.Usage("invalid_ruling_alias", "aliases must be distinct legacy names")
		}
		seen[alias] = true
	}
	return nil
}

type rulingWrite struct {
	ID               string           `json:"ruling_id"`
	Aliases          []string         `json:"aliases,omitempty"`
	Scope            string           `json:"scope"`
	Topic            string           `json:"topic"`
	Title            string           `json:"title"`
	Body             string           `json:"body"`
	Quote            string           `json:"source_quote"`
	Source           string           `json:"source,omitempty"`
	StatusHint       string           `json:"status_hint,omitempty"`
	SupersededByHint string           `json:"superseded_by_hint,omitempty"`
	Relations        []rulingRelation `json:"relations,omitempty"`
	RequestKey       string           `json:"idempotency_key"`
}

type rulingRelation struct {
	Kind string `json:"kind"`
	ID   string `json:"ruling_id"`
}

func (a *App) proposeRuling(ctx context.Context, cfg config.Resolved, args rulingProposalArgs) (*commandResult, error) {
	body, err := a.readRulingInput(args.bodyFile, 32768*utf8.UTFMax)
	if err != nil {
		return nil, err
	}
	title := strings.TrimSpace(args.title)
	if title == "" {
		line, _, _ := strings.Cut(strings.TrimSpace(string(body)), "\n")
		title = strings.TrimSpace(strings.TrimLeft(line, "#"))
	}
	if title == "" || utf8.RuneCountInString(title) > 1024 || strings.TrimSpace(string(body)) == "" || utf8.RuneCount(body) > 32768 || utf8.RuneCountInString(args.quote) > 8192 || len(args.aliases) > 32 || len(args.relations) > 200 {
		return nil, errnorm.Usage("invalid_ruling_proposal", "title, body, quote, aliases or relations exceed the proposal limits")
	}
	req := rulingWrite{ID: args.date, Title: title, Body: string(body), Quote: args.quote, Scope: args.scope, Topic: args.topic, Aliases: args.aliases}
	for _, raw := range args.relations {
		kind, id, _ := strings.Cut(raw, ":")
		req.Relations = append(req.Relations, rulingRelation{Kind: kind, ID: id})
	}
	snapshot, err := a.collectRulings(ctx, cfg, true, false)
	if err != nil {
		return nil, err
	}
	selected := anyString(snapshot.projections[0]["workspace_url"])
	if !validRulingDateID(req.ID) {
		prefix := "R-" + strings.ReplaceAll(req.ID, "-", "") + "-"
		targets := make(map[string]bool, len(req.Relations))
		for _, rel := range req.Relations {
			targets[rel.ID] = true
		}
		req.ID = ""
		for n := 1; n <= 99; n++ {
			id := fmt.Sprintf("%s%02d", prefix, n)
			if targets[id] {
				continue
			}
			if home := snapshot.namespaces[id]; home == "" {
				if req.ID == "" {
					req.ID = id
				}
			} else if home == selected && sameRulingProposal(snapshot.names[id], req) {
				req.ID = id
				break
			}
		}
		if req.ID == "" {
			return nil, errnorm.Usage("ruling_date_exhausted", "all ruling IDs for this date are taken")
		}
	}
	if err := checkRulingWriteNames(snapshot, req, selected); err != nil {
		return nil, err
	}
	ruling, err := a.sendRulingProposal(ctx, cfg, "rulings propose", req)
	if err != nil {
		return nil, err
	}
	return a.rulingsOutput(cfg, map[string]any{"ruling": ruling}, []string{formatRuling(ruling, false)}, snapshot), nil
}

func sameRulingProposal(row map[string]any, req rulingWrite) bool {
	if row["status"] != "proposed" {
		return false
	}
	if row["body"] != req.Body || row["scope"] != req.Scope || row["topic"] != req.Topic || row["source_quote"] != req.Quote || row["question"] != req.Title {
		return false
	}
	aliases := asSlice(row["aliases"])
	relations := asSlice(row["relations"])
	if len(aliases) != len(req.Aliases) || len(relations) != len(req.Relations) {
		return false
	}
	for i, alias := range req.Aliases {
		if aliases[i] != alias {
			return false
		}
	}
	for i, rel := range req.Relations {
		got := asMap(relations[i])
		if got["kind"] != rel.Kind || got["ruling_id"] != rel.ID {
			return false
		}
	}
	return true
}

func checkRulingWriteNames(snapshot rulingCollection, req rulingWrite, selected string) error {
	for _, name := range append([]string{req.ID}, req.Aliases...) {
		if homes := snapshot.collisions[name]; len(homes) > 0 {
			return errnorm.New(errnorm.KindRemote, "conflict", rulingNameCollisionWarning(name, homes).Message)
		}
		if row := snapshot.names[name]; row != nil && row["kind"] != "operating" {
			return errnorm.New(errnorm.KindRemote, "conflict", "ruling name "+name+" belongs to an existing answer ruling")
		}
		if home := snapshot.namespaces[name]; home != "" && home != selected {
			return errnorm.New(errnorm.KindRemote, "conflict", "ruling name "+name+" is already pinned in "+home)
		}
	}
	return nil
}

func (a *App) sendRulingProposal(ctx context.Context, cfg config.Resolved, command string, req rulingWrite) (map[string]any, error) {
	canonical, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	req.RequestKey = fmt.Sprintf("ruling:%x", sha256.Sum256(canonical))
	result, err := a.invokeRawJSON(ctx, cfg, command, http.MethodPost, "/rulings/propose", req)
	if err != nil {
		return nil, err
	}
	body := commandResultBody(result)
	if body["redirected"] == "needs_relation" {
		var missing []string
		for _, raw := range asSlice(body["missing_relations"]) {
			missing = append(missing, anyString(raw))
		}
		message := "proposal requires a relation to an active ruling in this topic"
		if len(missing) > 0 {
			message = "proposal requires relations to " + strings.Join(missing, ", ")
		}
		return nil, errnorm.Usage("ruling_relation_required", message)
	}
	ruling := asMap(body["ruling"])
	if ruling == nil {
		return nil, errnorm.Local("rulings_proposal_invalid", "proposal response is missing the ruling")
	}
	return ruling, nil
}

type rulingIndexEntry struct {
	ID           string   `json:"id"`
	Aliases      []string `json:"aliases"`
	Status       string   `json:"status"`
	SupersededBy string   `json:"superseded_by"`
	Supersedes   []string `json:"supersedes"`
	Title        string   `json:"title"`
	Date         string   `json:"date"`
	Source       string   `json:"source"`
	Origin       string   `json:"origin"`
}

const defaultRulingImportMax = 1000

func decodeRulingIndex(raw []byte, maxEntries int) ([]rulingIndexEntry, error) {
	var index struct {
		SchemaVersion int                `json:"schema_version"`
		GeneratedFrom []string           `json:"generated_from"`
		Rulings       []rulingIndexEntry `json:"rulings"`
	}
	if err := json.Unmarshal(raw, &index); err != nil {
		return nil, err
	}
	if index.SchemaVersion != 1 || len(index.GeneratedFrom) == 0 || index.Rulings == nil {
		return nil, fmt.Errorf("expected a generated schema_version 1 rulings index")
	}
	if len(index.Rulings) > maxEntries {
		return nil, fmt.Errorf("index has %d entries, exceeding --max-entries %d", len(index.Rulings), maxEntries)
	}
	names := map[string]bool{}
	entries := map[string]rulingIndexEntry{}
	for _, r := range index.Rulings {
		date, err := time.Parse(time.DateOnly, r.Date)
		if !validRulingDateID(r.ID) || err != nil || r.ID[2:10] != date.Format("20060102") || r.Aliases == nil || r.Supersedes == nil || len(r.Aliases) > 32 || len(r.Supersedes) > 200 || strings.TrimSpace(r.Title) == "" || utf8.RuneCountInString(r.Title) > 1024 || strings.TrimSpace(r.Source) == "" || utf8.RuneCountInString(r.Source) > 2048 {
			return nil, fmt.Errorf("invalid ruling entry %q", r.ID)
		}
		switch r.Status {
		case "active", "superseded", "revoked", "spent", "historical":
		default:
			return nil, fmt.Errorf("invalid status for %s", r.ID)
		}
		if (r.Status == "revoked" || r.Status == "spent") && len(r.Supersedes) != 0 {
			return nil, fmt.Errorf("retired ruling %s cannot supersede another ruling", r.ID)
		}
		switch r.Origin {
		case "rulings-file", "register", "wave3", "hostctl":
		default:
			return nil, fmt.Errorf("invalid origin for %s", r.ID)
		}
		if err := validateRulingAliases(r.Aliases); err != nil {
			return nil, err
		}
		for _, name := range append([]string{r.ID}, r.Aliases...) {
			if names[name] {
				return nil, fmt.Errorf("duplicate ruling ID or alias %q", name)
			}
			names[name] = true
		}
		entries[r.ID] = r
	}
	for _, r := range index.Rulings {
		seen := map[string]bool{r.ID: true}
		if r.SupersededBy != "" && (r.Status != "superseded" || r.SupersededBy == r.ID || entries[r.SupersededBy].ID == "") {
			return nil, fmt.Errorf("invalid superseder for %s", r.ID)
		}
		for _, target := range r.Supersedes {
			if seen[target] || entries[target].ID == "" {
				return nil, fmt.Errorf("invalid supersession from %s to %s", r.ID, target)
			}
			seen[target] = true
		}
	}
	sort.Slice(index.Rulings, func(i, j int) bool { return index.Rulings[i].ID < index.Rulings[j].ID })
	return index.Rulings, nil
}

func (a *App) importRulings(ctx context.Context, cfg config.Resolved, path string, maxEntries int) (*commandResult, error) {
	raw, err := a.readRulingInput(path, rulingProjectionSize)
	if err != nil {
		return nil, err
	}
	entries, err := decodeRulingIndex(raw, maxEntries)
	if err != nil {
		return nil, errnorm.Usage("invalid_rulings_index", err.Error())
	}
	snapshot, err := a.collectRulings(ctx, cfg, true, false)
	if err != nil {
		return nil, err
	}
	selected := anyString(snapshot.projections[0]["workspace_url"])
	writes := make([]rulingWrite, 0, len(entries))
	for _, r := range entries {
		// The index describes provenance and lifecycle, not the ruling's prose
		// or topic. Use its heading as review evidence and keep the source link.
		req := rulingWrite{
			ID: r.ID, Aliases: r.Aliases, Scope: "account", Topic: "import-" + strings.ToLower(r.ID),
			Title: r.Title, Body: r.Title, Quote: r.Title, Source: r.Source,
			StatusHint: r.Status, SupersededByHint: r.SupersededBy,
		}
		for _, target := range r.Supersedes {
			req.Relations = append(req.Relations, rulingRelation{Kind: "supersedes", ID: target})
		}
		if err := checkRulingWriteNames(snapshot, req, selected); err != nil {
			return nil, err
		}
		writes = append(writes, req)
	}
	result := []any{}
	for _, req := range writes {
		sent, err := a.sendRulingProposal(ctx, cfg, "rulings import", req)
		if err != nil {
			return nil, fmt.Errorf("import %s after %d successful submissions (safe to retry the same index): %w", req.ID, len(result), err)
		}
		result = append(result, sent)
	}
	return &commandResult{Data: map[string]any{"rulings": result, "submitted": len(result)}, Warnings: snapshot.warnings}, nil
}

func (a *App) readRulingInput(path string, max int64) ([]byte, error) {
	reader := a.Stdin
	if path != "-" {
		file, err := os.Open(path)
		if err != nil {
			return nil, errnorm.Usage("rulings_input_unreadable", err.Error())
		}
		defer file.Close()
		reader = file
	}
	raw, err := io.ReadAll(io.LimitReader(reader, max+1))
	if err != nil {
		return nil, errnorm.Usage("rulings_input_unreadable", err.Error())
	}
	if int64(len(raw)) > max {
		return nil, errnorm.Usage("rulings_input_too_large", "ruling input exceeds size limit")
	}
	return raw, nil
}
