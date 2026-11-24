package hooks

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/kevindurb/tsgit/internal/repos"
)

// pushPayload mirrors the subset of a GitHub push event that receivers
// generally consume. Field order is stable so payloads diff cleanly in tests.
type pushPayload struct {
	Ref        string `json:"ref"`
	Before     string `json:"before"`
	After      string `json:"after"`
	Created    bool   `json:"created"`
	Deleted    bool   `json:"deleted"`
	Forced     bool   `json:"forced"`
	Repository struct {
		Name     string `json:"name"`
		FullName string `json:"full_name"`
		CloneURL string `json:"clone_url"`
	} `json:"repository"`
	Pusher struct {
		Name string `json:"name"`
	} `json:"pusher"`
}

// buildPushPayload builds the delivery body for one changed ref. baseURL is
// the node's clone base (e.g. "https://tsgit.example.ts.net"); repo is the
// canonical "namespace/name"; pusher is the pusher's login, or node name for
// tagged machines without a user.
func buildPushPayload(baseURL, repo string, change repos.RefChange, pusher string) ([]byte, error) {
	var p pushPayload
	p.Ref = change.Ref
	p.Before = change.Old
	p.After = change.New
	p.Created = change.Kind == repos.RefCreated
	p.Deleted = change.Kind == repos.RefDeleted
	p.Forced = change.Forced
	p.Repository.Name = repo[strings.LastIndex(repo, "/")+1:]
	p.Repository.FullName = repo
	p.Repository.CloneURL = strings.TrimSuffix(baseURL, "/") + "/" + repo + ".git"
	p.Pusher.Name = pusher
	return json.Marshal(p)
}

// signBody returns the value for X-Hub-Signature-256: sha256=<hex>.
func signBody(secret, body []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// newDeliveryID returns an RFC 4122 version-4 UUID for X-GitHub-Delivery.
func newDeliveryID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return hex.EncodeToString(b[0:4]) + "-" + hex.EncodeToString(b[4:6]) + "-" +
		hex.EncodeToString(b[6:8]) + "-" + hex.EncodeToString(b[8:10]) + "-" +
		hex.EncodeToString(b[10:]), nil
}
