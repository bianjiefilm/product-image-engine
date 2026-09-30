package httpapi

import "net/http"

const (
	unusableCandidateCode = "not_usable_candidate"
	unusableCandidateMsg  = "不保真结果不能作为可用候选"
)

func rejectUnusableCandidate(w http.ResponseWriter) {
	writeErr(w, http.StatusConflict, unusableCandidateCode, unusableCandidateMsg)
}
