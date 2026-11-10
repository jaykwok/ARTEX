// Package recipes implements ARTEX's strict argv-only recipe format (version 1).
package recipes

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Autumn-27/artex/config"
	"github.com/Autumn-27/artex/internal/toolrun"
	"github.com/Autumn-27/norma/permission"
	actool "github.com/Autumn-27/norma/tool"
	"gopkg.in/yaml.v3"
)

type Param struct {
	Name        string   `yaml:"name"`
	Type        string   `yaml:"type"`
	Description string   `yaml:"description"`
	Flag        string   `yaml:"flag"`
	Position    *int     `yaml:"position"`
	Combined    bool     `yaml:"combined"`
	Required    bool     `yaml:"required"`
	Minimum     *float64 `yaml:"minimum"`
	Maximum     *float64 `yaml:"maximum"`
	Enum        []string `yaml:"enum"`
}
type Recipe struct {
	Version          int      `yaml:"version"`
	Name             string   `yaml:"name"`
	Command          string   `yaml:"command"`
	Args             []string `yaml:"args"`
	Enabled          bool     `yaml:"enabled"`
	Agents           []string `yaml:"agents"`
	ShortDescription string   `yaml:"short_description"`
	Description      string   `yaml:"description"`
	Parameters       []Param  `yaml:"parameters"`
}

var namePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

func Load(dir string) ([]Recipe, []error) {
	paths, _ := filepath.Glob(filepath.Join(dir, "*.yaml"))
	var out []Recipe
	var errs []error
	seen := map[string]bool{}
	for _, path := range paths {
		f, err := os.Open(path)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		st, statErr := f.Stat()
		if statErr != nil || !st.Mode().IsRegular() || st.Size() > 128<<10 {
			f.Close()
			errs = append(errs, fmt.Errorf("%s: recipe must be a regular file <=128 KiB", path))
			continue
		}
		d := yaml.NewDecoder(io.LimitReader(f, 128<<10))
		d.KnownFields(true)
		var r Recipe
		err = d.Decode(&r)
		var extra any
		if err == nil && d.Decode(&extra) != io.EOF {
			err = fmt.Errorf("multiple YAML documents are not supported")
		}
		f.Close()
		if err == nil {
			err = r.Validate()
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", path, err))
			continue
		}
		if seen[r.Name] {
			errs = append(errs, fmt.Errorf("duplicate recipe %s", r.Name))
			continue
		}
		seen[r.Name] = true
		if !r.Enabled {
			continue
		}
		if _, err = exec.LookPath(r.Command); err != nil {
			errs = append(errs, fmt.Errorf("recipe %s unavailable: %w", r.Name, err))
			continue
		}
		out = append(out, r)
	}
	return out, errs
}
func (r Recipe) Validate() error {
	if r.Version != 1 || !namePattern.MatchString(r.Name) || r.Command == "" || strings.ContainsAny(r.Command, "\x00\r\n") || r.ShortDescription == "" || len([]rune(r.ShortDescription)) > 200 {
		return fmt.Errorf("version, name, command and short_description required")
	}
	seen := map[string]bool{}
	positions := map[int]bool{}
	for _, p := range r.Parameters {
		if !namePattern.MatchString(p.Name) || seen[p.Name] {
			return fmt.Errorf("invalid/duplicate parameter %s", p.Name)
		}
		seen[p.Name] = true
		if p.Type != "string" && p.Type != "integer" && p.Type != "boolean" {
			return fmt.Errorf("unsupported parameter type %s", p.Type)
		}
		if (p.Minimum != nil || p.Maximum != nil) && p.Type != "integer" {
			return fmt.Errorf("numeric bounds require integer type")
		}
		for _, bound := range []*float64{p.Minimum, p.Maximum} {
			if bound != nil && (math.IsNaN(*bound) || math.IsInf(*bound, 0)) {
				return fmt.Errorf("invalid numeric bound")
			}
		}
		if p.Minimum != nil && p.Maximum != nil && *p.Minimum > *p.Maximum {
			return fmt.Errorf("minimum exceeds maximum")
		}
		if p.Type == "boolean" && (len(p.Enum) > 0 || p.Combined) {
			return fmt.Errorf("boolean flags cannot use enum or combined")
		}
		if p.Type == "integer" {
			for _, choice := range p.Enum {
				if _, err := strconv.ParseInt(choice, 10, 64); err != nil {
					return fmt.Errorf("invalid integer enum")
				}
			}
		}
		if (p.Position == nil) == (p.Flag == "") {
			return fmt.Errorf("parameter %s needs exactly one flag or position", p.Name)
		}
		if p.Position != nil {
			if *p.Position < 0 || positions[*p.Position] || p.Type == "boolean" || p.Combined {
				return fmt.Errorf("invalid positional parameter")
			}
			positions[*p.Position] = true
		}
		if p.Flag != "" && !strings.HasPrefix(p.Flag, "-") {
			return fmt.Errorf("invalid flag")
		}
	}
	return nil
}
func (r Recipe) Argv(in json.RawMessage) ([]string, error) {
	var values map[string]any
	if json.Unmarshal(in, &values) != nil || values == nil {
		return nil, fmt.Errorf("arguments must be an object")
	}
	known := map[string]bool{}
	args := append([]string(nil), r.Args...)
	params := append([]Param(nil), r.Parameters...)
	sort.SliceStable(params, func(i, j int) bool {
		a, b := params[i].Position, params[j].Position
		if a == nil {
			return b != nil
		}
		return b != nil && *a < *b
	})
	for _, p := range params {
		known[p.Name] = true
		v, ok := values[p.Name]
		if !ok {
			if p.Required {
				return nil, fmt.Errorf("%s is required", p.Name)
			}
			continue
		}
		var text string
		switch p.Type {
		case "boolean":
			b, ok := v.(bool)
			if !ok {
				return nil, fmt.Errorf("%s must be boolean", p.Name)
			}
			if b {
				args = append(args, p.Flag)
			}
			continue
		case "integer":
			n, ok := v.(float64)
			if !ok || n != float64(int64(n)) || (p.Minimum != nil && n < *p.Minimum) || (p.Maximum != nil && n > *p.Maximum) {
				return nil, fmt.Errorf("%s is outside its integer contract", p.Name)
			}
			text = strconv.FormatInt(int64(n), 10)
		default:
			var ok bool
			text, ok = v.(string)
			if !ok || strings.ContainsRune(text, 0) || len(text) > 8192 {
				return nil, fmt.Errorf("invalid string %s", p.Name)
			}
		}
		if len(p.Enum) > 0 {
			found := false
			for _, e := range p.Enum {
				if e == text {
					found = true
				}
			}
			if !found {
				return nil, fmt.Errorf("invalid %s choice", p.Name)
			}
		}
		if p.Position != nil {
			if strings.HasPrefix(text, "-") {
				return nil, fmt.Errorf("positional argument cannot begin with a flag")
			}
			args = append(args, text)
		} else if p.Combined {
			args = append(args, p.Flag+"="+text)
		} else {
			args = append(args, p.Flag, text)
		}
	}
	for k := range values {
		if !known[k] {
			return nil, fmt.Errorf("unknown parameter %s", k)
		}
	}
	return args, nil
}

type recipeTool struct {
	actool.CoreTool
	r Recipe
}

func (t *recipeTool) ShortDescription() string { return t.r.ShortDescription }

func (t *recipeTool) AllowedAgent(agent string) bool {
	for _, a := range t.r.Agents {
		if a == agent {
			return true
		}
	}
	return len(t.r.Agents) == 0 && (agent == "worker" || agent == "retester")
}
func (r Recipe) Tool() actool.CoreTool {
	props := map[string]any{}
	required := []string{}
	for _, p := range r.Parameters {
		v := map[string]any{"type": p.Type, "description": p.Description}
		if p.Minimum != nil {
			v["minimum"] = *p.Minimum
		}
		if p.Maximum != nil {
			v["maximum"] = *p.Maximum
		}
		if len(p.Enum) > 0 {
			values := make([]any, 0, len(p.Enum))
			for _, choice := range p.Enum {
				if p.Type == "integer" {
					n, _ := strconv.ParseInt(choice, 10, 64)
					values = append(values, n)
				} else {
					values = append(values, choice)
				}
			}
			v["enum"] = values
		}
		props[p.Name] = v
		if p.Required {
			required = append(required, p.Name)
		}
	}
	t := actool.Build(actool.Spec{Name: "recipe__" + r.Name, Description: r.ShortDescription + "\n" + r.Description, Schema: map[string]any{"type": "object", "properties": props, "required": required, "additionalProperties": false}, Permissions: func(context.Context, json.RawMessage, permission.Context) permission.Decision {
		return permission.AskUser("execute local recipe " + r.Name + "?")
	}, Run: func(ctx context.Context, in json.RawMessage, tc *actool.ToolContext) (actool.Result, error) {
		args, err := r.Argv(in)
		if err != nil {
			return actool.Errorf(err.Error()), nil
		}
		return toolrun.RunCommand(ctx, r.Command, args, tc, time.Duration(config.RuntimeOptions().NoOutputSeconds)*time.Second)
	}})
	return &recipeTool{CoreTool: t, r: r}
}
