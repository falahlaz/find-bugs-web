package api

import (
	"net/http"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/falahlaz/find-bugs-web/apps/server/internal/platform/httpx"
)

// The OpenAPI document is generated from the route table and the Go request
// and response types, so it cannot drift from the handlers.

type schema = map[string]any

type specBuilder struct {
	components map[string]schema
}

var timeType = reflect.TypeOf(time.Time{})

func (b *specBuilder) schemaFor(t reflect.Type) schema {
	nullable := false
	for t.Kind() == reflect.Pointer {
		t, nullable = t.Elem(), true
	}
	var s schema
	switch {
	case t == timeType:
		s = schema{"type": "string", "format": "date-time"}
	case t.Kind() == reflect.Struct:
		name := t.Name()
		if _, ok := b.components[name]; !ok {
			b.components[name] = nil // reserve against recursion
			b.components[name] = b.structSchema(t)
		}
		s = schema{"$ref": "#/components/schemas/" + name}
		if nullable {
			return schema{"allOf": []any{s}, "nullable": true}
		}
		return s
	case t.Kind() == reflect.Slice:
		s = schema{"type": "array", "items": b.schemaFor(t.Elem())}
	case t.Kind() == reflect.Map:
		s = schema{"type": "object", "additionalProperties": b.schemaFor(t.Elem())}
	case t.Kind() == reflect.String:
		s = schema{"type": "string"}
		if e, ok := reflect.Zero(t).Interface().(interface{ EnumValues() []string }); ok {
			s["enum"] = e.EnumValues()
		}
	case t.Kind() == reflect.Bool:
		s = schema{"type": "boolean"}
	case t.Kind() >= reflect.Int && t.Kind() <= reflect.Uint64:
		s = schema{"type": "integer", "format": "int64"}
	case t.Kind() == reflect.Float32 || t.Kind() == reflect.Float64:
		s = schema{"type": "number"}
	default:
		s = schema{}
	}
	if nullable {
		s["nullable"] = true
	}
	return s
}

func (b *specBuilder) structSchema(t reflect.Type) schema {
	props := schema{}
	var required []string
	var walk func(reflect.Type)
	walk = func(t reflect.Type) {
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if !f.IsExported() {
				continue
			}
			tag := f.Tag.Get("json")
			if tag == "-" {
				continue
			}
			name, opts, _ := strings.Cut(tag, ",")
			if f.Anonymous && name == "" {
				ft := f.Type
				if ft.Kind() == reflect.Pointer {
					ft = ft.Elem()
				}
				walk(ft)
				continue
			}
			if name == "" {
				name = f.Name
			}
			ps := b.schemaFor(f.Type)
			if d := f.Tag.Get("doc"); d != "" {
				if _, isRef := ps["$ref"]; isRef {
					ps = schema{"allOf": []any{ps}, "description": d}
				} else {
					ps["description"] = d
				}
			}
			if e := f.Tag.Get("enum"); e != "" {
				ps["enum"] = strings.Split(e, ",")
			}
			props[name] = ps
			if !strings.Contains(opts, "omitempty") && f.Type.Kind() != reflect.Pointer {
				required = append(required, name)
			}
		}
	}
	walk(t)
	s := schema{"type": "object", "properties": props}
	if len(required) > 0 {
		sort.Strings(required)
		s["required"] = required
	}
	return s
}

var pathParam = regexp.MustCompile(`\{([a-zA-Z]+)\}`)

func (a *API) openAPI() schema {
	b := &specBuilder{components: map[string]schema{}}
	errRef := b.schemaFor(reflect.TypeOf(httpx.ErrorBody{}))
	paths := schema{}
	for _, r := range a.routes {
		if r.raw {
			continue
		}
		op := schema{"summary": r.summary, "tags": []string{r.tag}, "operationId": r.opID}
		var params []any
		for _, m := range pathParam.FindAllStringSubmatch(r.path, -1) {
			// {id} is a numeric row ID; other names ({tool}, {repo}) are strings.
			typ := schema{"type": "string"}
			if m[1] == "id" {
				typ = schema{"type": "integer", "format": "int64"}
			}
			params = append(params, schema{"name": m[1], "in": "path", "required": true, "schema": typ})
		}
		for _, q := range r.query {
			params = append(params, schema{"name": q, "in": "query", "required": false, "schema": schema{"type": "string"}})
		}
		if len(params) > 0 {
			op["parameters"] = params
		}
		if r.req != nil {
			op["requestBody"] = schema{"required": true, "content": schema{"application/json": schema{"schema": b.schemaFor(reflect.TypeOf(r.req))}}}
		}
		resps := schema{"default": schema{"description": "Error", "content": schema{"application/json": schema{"schema": errRef}}}}
		for code, v := range r.resps {
			resp := schema{"description": http.StatusText(code)}
			if v != nil {
				resp["content"] = schema{"application/json": schema{"schema": b.schemaFor(reflect.TypeOf(v))}}
			}
			resps[strconv.Itoa(code)] = resp
		}
		op["responses"] = resps
		if !r.public {
			op["security"] = []any{schema{"session": []any{}}}
			if len(r.roles) > 0 {
				op["x-roles"] = r.roles
			}
			if r.menu != "" {
				op["x-menu"] = r.menu
			}
		}
		item, _ := paths[r.path].(schema)
		if item == nil {
			item = schema{}
			paths[r.path] = item
		}
		item[strings.ToLower(r.method)] = op
	}
	return schema{
		"openapi": "3.0.3",
		"info": schema{
			"title":       "Find Bugs Web API",
			"version":     a.Version,
			"description": "Unsafe methods need the X-CSRF-Token header returned by /api/auth/login or /api/me.",
		},
		"paths": paths,
		"components": schema{
			"schemas": b.components,
			"securitySchemes": schema{
				"session": schema{"type": "apiKey", "in": "cookie", "name": "fbw_session"},
			},
		},
	}
}
