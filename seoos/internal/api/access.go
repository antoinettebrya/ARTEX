package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/Autumn-27/artex/seoos/internal/domain"
)

func forbidden(w http.ResponseWriter) {
	writeError(w, http.StatusForbidden, "forbidden", "The current role cannot perform this action.")
}

func (h *handler) serveAccess(w http.ResponseWriter, r *http.Request, principal domain.Principal) bool {
	path := r.URL.Path
	if path == "/api/v1/me" {
		if !readMethod(r.Method) {
			methodNotAllowed(w, "GET, HEAD")
		} else if checkQuery(w, r) {
			writeJSON(w, http.StatusOK, principal)
		}
		return true
	}
	const members = "/api/v1/members"
	if h.access == nil || (path != members && path != "/api/v1/audit" && !strings.HasPrefix(path, members+"/")) {
		return false
	}
	if principal.Role != domain.RoleOwner {
		forbidden(w)
		return true
	}
	switch path {
	case members:
		h.members(w, r, principal.OrganizationID)
	case "/api/v1/audit":
		if !readMethod(r.Method) {
			methodNotAllowed(w, "GET, HEAD")
			break
		}
		limit, offset, ok := readPage(w, r)
		if ok {
			entries, err := h.access.ListAudit(r.Context(), principal.OrganizationID, limit, offset)
			writeAccessResult(w, http.StatusOK, map[string]any{"events": entries}, err)
		}
	default:
		h.memberAction(w, r, principal.OrganizationID, strings.TrimPrefix(path, members+"/"))
	}
	return true
}

func (h *handler) members(w http.ResponseWriter, r *http.Request, orgID string) {
	if readMethod(r.Method) {
		limit, offset, ok := readPage(w, r)
		if ok {
			members, err := h.access.ListMembers(r.Context(), orgID, limit, offset)
			writeAccessResult(w, http.StatusOK, map[string]any{"members": members}, err)
		}
		return
	}
	if r.Method != http.MethodPost {
		methodNotAllowed(w, "GET, HEAD, POST")
		return
	}
	var input *struct {
		Email string      `json:"email"`
		Role  domain.Role `json:"role"`
	}
	if checkQuery(w, r) && readObject(w, r, &input) {
		credential, err := h.access.AddMember(r.Context(), orgID, input.Email, input.Role)
		writeAccessResult(w, http.StatusCreated, credential, err)
	}
}

func (h *handler) memberAction(w http.ResponseWriter, r *http.Request, orgID, path string) {
	userID, action, ok := strings.Cut(path, "/")
	if !ok || (action != "role" && action != "token") || !domain.ValidID(userID) {
		writeError(w, http.StatusNotFound, "not_found", "Member resource not found.")
		return
	}
	if !checkQuery(w, r) {
		return
	}
	if action == "role" {
		if r.Method != http.MethodPut {
			methodNotAllowed(w, "PUT")
			return
		}
		var input *struct {
			Role domain.Role `json:"role"`
		}
		if readObject(w, r, &input) {
			member, err := h.access.SetMemberRole(r.Context(), orgID, userID, input.Role)
			writeAccessResult(w, http.StatusOK, member, err)
		}
		return
	}
	switch r.Method {
	case http.MethodPost:
		credential, err := h.access.RotateMemberToken(r.Context(), orgID, userID)
		writeAccessResult(w, http.StatusOK, credential, err)
	case http.MethodDelete:
		if err := h.access.RevokeMemberToken(r.Context(), orgID, userID); err != nil {
			writeAccessResult(w, 0, nil, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		methodNotAllowed(w, "POST, DELETE")
	}
}

func readObject[T any](w http.ResponseWriter, r *http.Request, input **T) bool {
	if !readJSON(w, r, input) {
		return false
	}
	if *input == nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Request body must be a JSON object.")
		return false
	}
	return true
}

func readPage(w http.ResponseWriter, r *http.Request, filters ...string) (limit, offset int, ok bool) {
	if !checkQuery(w, r, append([]string{"limit", "offset"}, filters...)...) {
		return 0, 0, false
	}
	limit, err := queryInt(r, "limit", 50, 1, 200)
	if err == nil {
		offset, err = queryInt(r, "offset", 0, 0, 100000)
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return 0, 0, false
	}
	return limit, offset, true
}

func writeAccessResult(w http.ResponseWriter, status int, value any, err error) {
	switch {
	case err == nil:
		writeJSON(w, status, value)
	case errors.Is(err, domain.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "Resource not found.")
	case errors.Is(err, domain.ErrMemberExists):
		writeError(w, http.StatusConflict, "member_exists", "This user is already a member of the organization.")
	case errors.Is(err, domain.ErrLastOwner):
		writeError(w, http.StatusConflict, "last_owner", "The organization must retain an enabled owner.")
	default:
		writeRepositoryError(w, err)
	}
}
