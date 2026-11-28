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
	aliases                                    []string
	relations                                  []rulingRelation
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
		kind, id, _ := strings.Cut(value, ":")
		p.relations = append(p.relations, rulingRelation{Kind: kind, ID: id})
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
	if err := validateRulingRelations(p.date, p.relations); err != nil {
		return err
	}
	return validateRulingAliases(p.aliases)
}

func validateRulingRelations(id string, relations []rulingRelation) error {
	seen := map[string]bool{id: true}
	for _, rel := range relations {
		if !validRulingDateID(rel.ID) || seen[rel.ID] || (rel.Kind != "supersedes" && rel.Kind != "amends" && rel.Kind != "independent") {
			return errnorm.Usage("invalid_ruling_relation", "use one supersedes, amends or independent relation per other R-YYYYMMDD-NN")
		}
		seen[rel.ID] = true
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

type rulingProposal struct {
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
}

type rulingWrite struct {
	rulingProposal
	RequestKey string `json:"idempotency_key"`
}

type rulingRelation struct {
	Kind string `json:"kind"`
	ID   string `json:"ruling_id"`
}

func (r rulingProposal) validateContent() error {
	for _, field := range []struct {
		name, value string
		max         int
	}{
		{"title", r.Title, 1024},
		{"body", r.Body, 32768},
		{"source_quote", r.Quote, 8192},
	} {
		if strings.TrimSpace(field.value) == "" || utf8.RuneCountInString(field.value) > field.max {
			return errnorm.Usage("invalid_ruling_proposal", fmt.Sprintf("%s must contain text and be at most %d characters", field.name, field.max))
		}
	}
	if len(r.Aliases) > 32 || len(r.Relations) > 200 {
		return errnorm.Usage("invalid_ruling_proposal", "at most 32 aliases and 200 relations are allowed")
	}
	return nil
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
	req := rulingProposal{ID: args.date, Title: title, Body: string(body), Quote: args.quote, Scope: args.scope, Topic: args.topic, Aliases: args.aliases, Relations: args.relations}
	if err := req.validateContent(); err != nil {
		return nil, err
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

func sameRulingProposal(row map[string]any, req rulingProposal) bool {
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

func checkRulingWriteNames(snapshot rulingCollection, req rulingProposal, selected string) error {
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

func (a *App) sendRulingProposal(ctx context.Context, cfg config.Resolved, command string, req rulingProposal) (map[string]any, error) {
	write := rulingWrite{rulingProposal: req}
	canonical, err := json.Marshal(write)
	if err != nil {
		return nil, err
	}
	write.RequestKey = fmt.Sprintf("ruling:%x", sha256.Sum256(canonical))
	result, err := a.invokeRawJSON(ctx, cfg, command, http.MethodPost, "/rulings/propose", write)
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
