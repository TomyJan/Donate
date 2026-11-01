// contractgen derives the wire schema from the Go JSON DTOs used by the server.
package main

import (
	"encoding/json"
	"fmt"
	"github.com/TomyJan/Donate/internal/admin"
	"github.com/TomyJan/Donate/internal/auth"
	"github.com/TomyJan/Donate/internal/config"
	"github.com/TomyJan/Donate/internal/geo"
	"github.com/TomyJan/Donate/internal/httpapi"
	"github.com/TomyJan/Donate/internal/order"
	"github.com/TomyJan/Donate/internal/payment"
	"os"
	"reflect"
	"slices"
	"strings"
	"time"
)

type schema = map[string]any

func describe(t reflect.Type) schema {
	if t.Kind() == reflect.Pointer {
		value := describe(t.Elem())
		value["nullable"] = true
		return value
	}
	if t == reflect.TypeFor[time.Time]() {
		return schema{"type": "string", "format": "date-time"}
	}
	switch t.Kind() {
	case reflect.String:
		return schema{"type": "string"}
	case reflect.Bool:
		return schema{"type": "boolean"}
	case reflect.Int, reflect.Int64:
		return schema{"type": "integer", "format": "int64"}
	case reflect.Float64:
		return schema{"type": "number"}
	case reflect.Slice:
		return schema{"type": "array", "items": describe(t.Elem())}
	case reflect.Map:
		return schema{"type": "object", "additionalProperties": describe(t.Elem())}
	case reflect.Struct:
		props := map[string]any{}
		required := []string{}
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
				if f.Anonymous && tag == "" {
					walk(f.Type)
					continue
				}
				name, option, _ := strings.Cut(tag, ",")
				if name == "" {
					name = f.Name
				}
				props[name] = describe(f.Type)
				if option != "omitempty" && !slices.Contains(required, name) {
					required = append(required, name)
				}
			}
		}
		walk(t)
		slices.Sort(required)
		return schema{"type": "object", "properties": props, "required": required}
	}
	panic("unsupported type " + t.String())
}
func main() {
	if err := os.WriteFile(".env.example", config.Example(), 0644); err != nil {
		panic(err)
	}
	types := map[string]reflect.Type{"Profile": reflect.TypeFor[auth.Profile](), "SiteVersion": reflect.TypeFor[httpapi.Version](), "Identity": reflect.TypeFor[auth.Identity](), "RecordItem": reflect.TypeFor[order.Record](), "Records": reflect.TypeFor[order.Page](), "Order": reflect.TypeFor[admin.Detail](), "OrderPage": reflect.TypeFor[admin.Page[admin.OrderRow]](), "User": reflect.TypeFor[admin.User](), "UserPage": reflect.TypeFor[admin.Page[admin.User]](), "Audit": reflect.TypeFor[admin.Audit](), "AuditPage": reflect.TypeFor[admin.Page[admin.Audit]](), "Overview": reflect.TypeFor[admin.Overview](), "Metric": reflect.TypeFor[admin.Metric](), "Operation": reflect.TypeFor[admin.Operation](), "Location": reflect.TypeFor[geo.Location](), "Checkout": reflect.TypeFor[payment.Checkout](), "FlowStatus": reflect.TypeFor[auth.FlowStatus]()}
	schemas := map[string]any{}
	for name, t := range map[string]reflect.Type{"TaskItem": reflect.TypeFor[admin.TaskItem](), "TaskDetail": reflect.TypeFor[admin.TaskDetail](), "TaskEvent": reflect.TypeFor[admin.TaskEvent](), "TaskCounts": reflect.TypeFor[admin.TaskCounts](), "TaskTrend": reflect.TypeFor[admin.TaskTrend](), "TaskOverview": reflect.TypeFor[admin.TaskOverview](), "TaskRetryItem": reflect.TypeFor[admin.TaskRetryItem](), "TaskRetryResult": reflect.TypeFor[admin.TaskRetryResult](), "TaskPage": reflect.TypeFor[admin.Page[admin.TaskItem]](), "TaskEventPage": reflect.TypeFor[admin.Page[admin.TaskEvent]]()} {
		types[name] = t
	}
	for name, t := range types {
		schemas[name] = describe(t)
	}
	identityProps := schemas["Identity"].(schema)["properties"].(map[string]any)
	identityProps["id"].(schema)["format"] = "uuid"
	identityProps["bound_at"].(schema)["description"] = "Original binding instant from identities.created_at; signing in again does not change it."
	recordProps := schemas["RecordItem"].(schema)["properties"].(map[string]any)
	recordProps["is_mine"].(schema)["description"] = "Whether this record belongs to the effective authenticated session. Always false for guests; no owner user ID is exposed. Public feeds still contain only paid, non-hidden orders."
	recordProps["province"].(schema)["description"] = "Stored province-level location only, or an empty string when lookup data is unavailable."
	orderProps := schemas["Order"].(schema)["properties"].(map[string]any)
	orderProps["province"].(schema)["description"] = "Province-level stored location, or empty when unavailable. Full location is restricted to the admin detail route."
	orderProps["ip"].(schema)["description"] = "The order's request IP: visible to its authenticated owner in ordinary detail and to administrators in admin detail. Never included in public lists or ordinary detail viewed by a non-owner administrator."
	orderProps["reconciliations"].(schema)["description"] = "Admin detail only: all persisted distinct manual reconciliation requests, newest first. IDs are stored request UUIDs, not idempotency keys. Several requests may share a job; status/result reflect the currently linked job. Absent from ordinary owner detail and after physical deletion."
	jobProps := orderProps["jobs"].(schema)["items"].(schema)["properties"].(map[string]any)
	jobProps["source"].(schema)["description"] = "The job's current wakeup source, such as automatic, manual, browser_return or admin_refresh. A geo job with admin_refresh forcibly rechecks current configured sources; automatic jobs must not be presented as manual requests."
	jobProps["created_at"].(schema)["description"] = "Job creation instant. This is not the timestamp of each manual request when an active job is reused."
	for _, name := range []string{"TaskItem", "TaskDetail"} {
		props := schemas[name].(schema)["properties"].(map[string]any)
		props["resource"].(schema)["description"] = "For geoip_update system jobs: xdb4, xdb6 or mmdb. Empty for order jobs. Download URLs and local paths are never exposed."
		props["order_id"].(schema)["description"] = "Order UUID for order jobs; empty for geoip_update system jobs."
	}
	auditProps := schemas["Audit"].(schema)["properties"].(map[string]any)
	for field, description := range map[string]string{
		"actor":             "Current actor nickname, or the stable system key when unavailable.",
		"target":            "Readable current target summary; empty when the target no longer exists. Never an ID or a retained order snapshot.",
		"target_type":       "Target kind: order, user, or task for a system-task retry.",
		"target_exists":     "Whether the target currently exists. Disable target details when false.",
		"target_nickname":   "Current target user nickname, or the current order owner's nickname when available. Absent for deleted targets.",
		"target_resource":   "Current system-task library resource: xdb4, xdb6 or mmdb. Absent for other kinds and deleted tasks.",
		"target_amount_fen": "Current order amount in fen. Absent for user targets and physically deleted orders.",
	} {
		auditProps[field].(schema)["description"] = description
	}
	ref := func(name string) schema { return schema{"$ref": "#/components/schemas/" + name} }
	schemas["Site"] = schema{"type": "object", "required": []string{"name", "logo", "version", "providers", "payment_enabled", "csrf_token"}, "properties": schema{"name": schema{"type": "string"}, "logo": schema{"type": "string"}, "version": ref("SiteVersion"), "providers": schema{"type": "array", "items": schema{"type": "string"}}, "payment_enabled": schema{"type": "boolean"}, "csrf_token": schema{"type": "string"}}}
	schemas["LoginProvider"] = describe(reflect.TypeFor[config.LoginProvider]())
	schemas["Site"].(schema)["properties"].(schema)["provider_details"] = schema{"type": "array", "items": ref("LoginProvider"), "description": "Public sign-in IDs, names and icons. kind is the stable stored identity type; use id for login, binding and callback routes."}

	requestTypes := map[string]reflect.Type{"LoginInput": reflect.TypeFor[httpapi.LoginInput](), "QQInput": reflect.TypeFor[httpapi.QQInput](), "CreateOrderInput": reflect.TypeFor[httpapi.CreateOrderInput](), "IDInput": reflect.TypeFor[httpapi.IDInput](), "OperationInput": reflect.TypeFor[httpapi.OperationInput](), "PreviewInput": reflect.TypeFor[httpapi.PreviewInput](), "ReconcileInput": reflect.TypeFor[httpapi.ReconcileInput]()}
	for name, t := range requestTypes {
		schemas[name] = describe(t)
	}
	schemas["TaskRetryInput"] = describe(reflect.TypeFor[httpapi.TaskRetryInput]())
	previewProps := schemas["PreviewInput"].(schema)["properties"].(map[string]any)
	previewProps["action"].(schema)["enum"] = []string{"close", "hide_order", "show_order", "hide_message", "show_message", "delete", "export", "refresh_location"}
	previewProps["action"].(schema)["description"] = "refresh_location queues a forced geo lookup after confirmation, including legacy orders. Success replaces the saved location, failure preserves it; payment state is unchanged. Missing targets return not_found, disabled geo mode returns disabled without queuing. Active refresh tasks are reused."
	schemas["UnbindIdentityInput"] = describe(reflect.TypeFor[httpapi.UnbindIdentityInput]())
	for _, name := range []string{"id", "identity_id"} {
		schemas["UnbindIdentityInput"].(schema)["properties"].(map[string]any)[name].(schema)["format"] = "uuid"
	}
	schemas["OrderBase"] = describe(reflect.TypeFor[order.Order]())
	schemas["LoginStart"] = describe(reflect.TypeFor[auth.FlowStart]())

	paths := map[string]any{}
	routes := []struct {
		method, path, result string
		auth                 bool
	}{{"get", "site/info", "Site", false}, {"get", "user/profile", "Profile", true}, {"post", "user/login", "", false}, {"get", "user/login/status", "FlowStatus", false}, {"post", "user/login/qq/complete", "", false}, {"post", "user/identity/bind", "", true}, {"post", "user/session/renew", "", true}, {"post", "user/logout", "", true}, {"get", "order/list", "Records", false}, {"get", "order/detail", "Order", true}, {"post", "order/create", "Order", true}, {"post", "order/payment/start", "Checkout", true}, {"get", "admin/order/list", "OrderPage", true}, {"get", "admin/order/detail", "Order", true}, {"post", "admin/order/action/preview", "Operation", true}, {"post", "admin/order/action/execute", "Operation", true}, {"post", "admin/order/reconcile", "", true}, {"get", "admin/operation/detail", "Operation", true}, {"get", "admin/user/list", "UserPage", true}, {"get", "admin/user/detail", "User", true}, {"post", "admin/user/ban", "", true}, {"post", "admin/user/unban", "", true}, {"post", "admin/user/identity/unbind", "", true}, {"get", "admin/overview", "Overview", true}, {"get", "admin/audit/list", "AuditPage", true}}
	routes = append(routes, []struct {
		method, path, result string
		auth                 bool
	}{{"get", "admin/task/list", "TaskPage", true}, {"get", "admin/task/detail", "TaskDetail", true}, {"get", "admin/task/history", "TaskEventPage", true}, {"get", "admin/task/overview", "TaskOverview", true}, {"post", "admin/task/retry", "TaskRetryResult", true}}...)
	for _, r := range routes {
		data := schema{}
		if r.result != "" {
			data = ref(r.result)
		}
		response := schema{"description": "Business response; inspect code. Basic authentication failures use 401/403.", "content": schema{"application/json": schema{"schema": schema{"type": "object", "required": []string{"code", "message", "data", "request_id"}, "properties": schema{"code": schema{"type": "string"}, "message": schema{"type": "string"}, "request_id": schema{"type": "string"}, "data": data}}}}}
		operation := schema{"operationId": strings.ReplaceAll(r.path, "/", "_") + "_" + r.method, "summary": r.path, "responses": schema{"200": response, "401": schema{"description": "Missing or invalid session"}, "403": schema{"description": "Forbidden, banned, or CSRF refused"}, "500": schema{"description": "Infrastructure failure"}}}
		if r.auth {
			operation["security"] = []any{schema{"session": []string{}}}
		}
		if r.method == "post" {
			operation["parameters"] = []any{schema{"in": "header", "name": "X-CSRF-Token", "required": true, "schema": schema{"type": "string"}}}
		}

		bodies := map[string]string{"user/login": "LoginInput", "user/identity/bind": "LoginInput", "user/login/qq/complete": "QQInput", "order/create": "CreateOrderInput", "order/payment/start": "IDInput", "admin/order/action/preview": "PreviewInput", "admin/order/action/execute": "OperationInput", "admin/order/reconcile": "ReconcileInput", "admin/user/ban": "IDInput", "admin/user/unban": "IDInput"}
		bodies["admin/user/identity/unbind"] = "UnbindIdentityInput"
		bodies["admin/task/retry"] = "TaskRetryInput"
		if body := bodies[r.path]; r.method == "post" && body != "" {
			operation["requestBody"] = schema{"required": true, "content": schema{"application/json": schema{"schema": ref(body)}}}
		}
		if strings.HasSuffix(r.path, "/detail") {
			operation["parameters"] = []any{schema{"in": "query", "name": "id", "required": true, "schema": schema{"type": "string", "format": "uuid"}}}
		}
		if r.path == "order/create" {
			data = ref("OrderBase")
			response["content"].(schema)["application/json"].(schema)["schema"].(schema)["properties"].(schema)["data"] = data
		}
		if r.path == "user/login" || r.path == "user/identity/bind" {
			response["content"].(schema)["application/json"].(schema)["schema"].(schema)["properties"].(schema)["data"] = ref("LoginStart")
		}
		if strings.HasSuffix(r.path, "/list") && strings.HasPrefix(r.path, "admin/") {
			params := []any{}
			for _, name := range []string{"search", "status", "channel", "visibility", "sort", "direction", "action", "user_id"} {
				params = append(params, parameter("query", name, false, schema{"type": "string"}))
			}
			for _, name := range []string{"from", "to"} {
				params = append(params, parameter("query", name, false, schema{"type": "string", "format": "date-time"}))
			}
			for _, name := range []string{"page", "page_size", "minimum", "maximum"} {
				params = append(params, parameter("query", name, false, schema{"type": "integer", "minimum": 0}))
			}
			operation["parameters"] = params
		}
		if r.path == "order/list" {
			operation["parameters"] = []any{parameter("query", "only_mine", false, schema{"type": "boolean", "default": false}), parameter("query", "cursor", false, schema{"type": "string"})}
		}
		if r.path == "admin/overview" {
			operation["description"] = "All cards and series use one read snapshot and the same selected [from,to) window. Revenue, paid count, daily and channels use paid_at; pending and closed use created_at and current status. Hidden orders count, physically deleted orders do not. Invalid ranges return the VALIDATION_FAILED business code."
			rangeParam := parameter("query", "range", false, schema{"type": "string", "enum": []string{"today", "yesterday", "7d", "30d", "90d", "all", "custom"}, "default": "30d"})
			rangeParam["description"] = "Shanghai calendar-day preset. Rolling presets include today and end at tomorrow's midnight; all is unbounded."
			fromParam := parameter("query", "from", false, schema{"type": "string", "format": "date-time"})
			fromParam["description"] = "Inclusive RFC3339 instant, required only for custom and forbidden for other ranges."
			toParam := parameter("query", "to", false, schema{"type": "string", "format": "date-time"})
			toParam["description"] = "Exclusive RFC3339 instant, required only for custom, later than from, and forbidden for other ranges."
			operation["parameters"] = []any{rangeParam, fromParam, toParam}
		}
		if r.path == "admin/order/list" {
			for _, value := range operation["parameters"].([]any) {
				param := value.(schema)
				if param["name"] == "sort" {
					param["schema"] = schema{"type": "string", "enum": []string{"id", "created_at", "nickname", "amount_fen", "channel", "message", "status", "hidden_order", "hidden_message", "paid_at"}}
				}
			}
		}
		if r.path == "user/login/qq/complete" {
			response["content"].(schema)["application/json"].(schema)["schema"].(schema)["properties"].(schema)["data"] = schema{"type": "object", "required": []string{"return_to"}, "properties": schema{"return_to": schema{"type": "string"}}}
		}
		if r.path == "admin/order/reconcile" {
			response["content"].(schema)["application/json"].(schema)["schema"].(schema)["properties"].(schema)["data"] = schema{"type": "object", "required": []string{"job_id"}, "properties": schema{"job_id": schema{"type": "string", "format": "uuid"}}}
		}
		if r.path == "admin/user/identity/unbind" {
			operation["description"] = "Administrator only. Removes the matched user's OAuth identity atomically, invalidates all credentials using it and cancels the user's outstanding binding flows. At least one login method must remain. Business failures LAST_IDENTITY_FORBIDDEN, NOT_FOUND and VALIDATION_FAILED use HTTP 200; inspect code. Success clears the cookie if the current session used the removed identity."
			response["content"].(schema)["application/json"].(schema)["schema"].(schema)["properties"].(schema)["data"] = schema{"nullable": true, "enum": []any{nil}}
		}
		if r.path == "admin/task/list" {
			params := []any{}
			for _, name := range []string{"status", "kind", "source", "search", "sort", "direction"} {
				params = append(params, parameter("query", name, false, schema{"type": "string"}))
			}
			for _, name := range []string{"from", "to"} {
				params = append(params, parameter("query", name, false, schema{"type": "string", "format": "date-time"}))
			}
			params = append(params, parameter("query", "page", false, schema{"type": "integer", "minimum": 1, "default": 1}), parameter("query", "page_size", false, schema{"type": "integer", "minimum": 1, "maximum": 100, "default": 20}))
			operation["parameters"] = params
		}
		if r.path == "admin/task/history" {
			operation["parameters"] = []any{parameter("query", "id", true, schema{"type": "string", "format": "uuid"}), parameter("query", "page", false, schema{"type": "integer", "minimum": 1, "default": 1}), parameter("query", "page_size", false, schema{"type": "integer", "minimum": 1, "maximum": 100, "default": 20})}
		}
		if r.path == "admin/task/overview" {
			operation["description"] = "Current-state counts are independent of trend range. Trend uses recorded events only (never upgrade snapshots), Shanghai calendar days, zero filling and at most 90 calendar-aligned buckets. Default range is 7d."
			operation["parameters"] = []any{parameter("query", "range", false, schema{"type": "string", "enum": []string{"today", "yesterday", "7d", "30d", "90d", "all", "custom"}, "default": "7d"}), parameter("query", "from", false, schema{"type": "string", "format": "date-time"}), parameter("query", "to", false, schema{"type": "string", "format": "date-time"})}
		}
		if r.path == "admin/task/retry" {
			operation["description"] = "Retries original failed tasks, resetting their current retry budget, or advances scheduled ready tasks without resetting it. Running and completed tasks are never replayed. A persistent caller-scoped idempotency key returns the original response; changed IDs conflict. At most 100 IDs. Permission is rechecked and order-target task.retry audit is written in the transaction."
		}
		if r.method == "get" && r.path != "site/info" && r.path != "user/profile" && r.path != "user/login/status" {
			params, _ := operation["parameters"].([]any)
			scope := parameter("header", "X-Session-Scope", false, schema{"type": "string"})
			scope["description"] = "Expected authenticated profile csrf_token, or guest for an anonymous read. A mismatch with the actual Cookie session returns 403 CSRF_FAILED and X-Session-Mismatch: 1 before data is read; rediscover user/profile. Omit only for legacy-client compatibility. This never grants authentication or admin access."
			operation["parameters"] = append(params, scope)
			operation["responses"].(schema)["403"].(schema)["headers"] = schema{"X-Session-Mismatch": schema{"description": "Present only when the expected read session differs from the actual Cookie session. Revalidate profile without clearing a newer observed session.", "schema": schema{"type": "string", "enum": []string{"1"}}}}
		}
		paths["/api/v1/"+r.path] = schema{r.method: operation}
	}
	textResponse := func(description, contentType string) schema {
		return schema{"description": description, "content": schema{contentType: schema{"schema": schema{"type": "string"}}}}
	}
	notificationParameters := []any{}
	notificationProperties := schema{}
	for _, name := range []string{"pid", "out_trade_no", "trade_no", "api_trade_no", "type", "money", "trade_status", "sign", "sign_type", "timestamp"} {
		notificationParameters = append(notificationParameters, parameter("query", name, false, schema{"type": "string"}))
		notificationProperties[name] = schema{"type": "string"}
	}
	paths["/api/v1/payment/notify"] = schema{
		"get":  schema{"operationId": "payment_notify_get", "summary": "Signed EPay notification; ACK only after commit", "parameters": notificationParameters, "responses": schema{"200": textResponse("success or fail", "text/plain"), "500": textResponse("Persistence failed; gateway must retry", "text/plain")}},
		"post": schema{"operationId": "payment_notify_post", "summary": "Signed EPay form notification; no CSRF header", "requestBody": schema{"required": true, "content": schema{"application/x-www-form-urlencoded": schema{"schema": schema{"type": "object", "properties": notificationProperties, "additionalProperties": schema{"type": "string"}}}}}, "responses": schema{"200": textResponse("success or fail", "text/plain"), "500": textResponse("Persistence failed", "text/plain")}},
	}
	redirect := schema{"description": "Redirect to a fixed same-origin page", "headers": schema{"Location": schema{"schema": schema{"type": "string"}}}}
	paths["/api/v1/payment/return"] = schema{"get": schema{"operationId": "payment_return_get", "summary": "Browser return wakes verification; never proves payment", "parameters": notificationParameters, "responses": schema{"303": redirect}}}
	paths["/api/v1/user/oauth/callback/{provider}"] = schema{"get": schema{"operationId": "user_oauth_callback_get", "summary": "OAuth callback with browser-bound flow cookie", "parameters": []any{parameter("path", "provider", true, schema{"type": "string", "description": "QQ or configured OIDC public provider ID from site/info.provider_details; use id, not the stable identity kind. Only enabled providers are accepted by the server."}), parameter("query", "code", false, schema{"type": "string"}), parameter("query", "state", false, schema{"type": "string"}), parameter("query", "error", false, schema{"type": "string"})}, "responses": schema{"303": redirect, "200": schema{"description": "Invalid callback parameters"}}}}
	paths["/api/v1/admin/order/export"] = schema{"post": schema{"operationId": "admin_order_export_post", "summary": "Export only the confirmed selected-order operation", "security": []any{schema{"session": []string{}}}, "parameters": []any{parameter("header", "X-CSRF-Token", true, schema{"type": "string"})}, "requestBody": schema{"required": true, "content": schema{"application/json": schema{"schema": ref("OperationInput")}}}, "responses": schema{"200": schema{"description": "UTF-8 BOM CSV on success; JSON business error otherwise", "content": schema{"text/csv": schema{"schema": schema{"type": "string"}}, "application/json": schema{"schema": schema{"type": "object"}}}}, "401": schema{"description": "Unauthenticated"}, "403": schema{"description": "Forbidden or invalid CSRF"}}}}
	for _, name := range []string{"live", "ready"} {
		paths["/health/"+name] = schema{"get": schema{"operationId": "health_" + name, "responses": schema{"200": schema{"description": "Healthy JSON envelope"}, "503": schema{"description": "Not ready"}}}}
	}
	spec := schema{"openapi": "3.0.3", "info": schema{"title": "Donate API", "version": "1.0.0", "description": "Same-origin semantic GET/POST API. Write requests require Origin and X-CSRF-Token. See docs/backend-design.md for permissions and business codes."}, "paths": paths, "components": schema{"schemas": schemas, "securitySchemes": schema{"session": schema{"type": "apiKey", "in": "cookie", "name": "__Host-donate_session"}}}}
	b, e := json.MarshalIndent(spec, "", "  ")
	if e != nil {
		panic(e)
	}
	if e = os.MkdirAll("api", 0755); e != nil {
		panic(e)
	}
	if e = os.WriteFile("api/openapi.json", append(b, '\n'), 0644); e != nil {
		panic(e)
	}
	fmt.Println("Generated API wire schemas")
}

func parameter(in, name string, required bool, value schema) schema {
	return schema{"in": in, "name": name, "required": required, "schema": value}
}
