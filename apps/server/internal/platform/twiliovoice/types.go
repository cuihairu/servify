package twiliovoice

import (
	"fmt"
	"net/url"
	"sort"
)

// webhookSignatureHeader is the header Twilio uses to carry the base64
// HMAC-SHA1 signature of the full request URL plus sorted form parameters.
const webhookSignatureHeader = "X-Twilio-Signature"

// formFromPayload normalizes the accepted payload shapes into url.Values.
// The dedicated webhook handler passes url.Values directly; the generic
// protocol entry points may hand over a pre-parsed map.
func formFromPayload(payload interface{}) (url.Values, error) {
	switch form := payload.(type) {
	case url.Values:
		return form, nil
	case map[string]string:
		values := make(url.Values, len(form))
		for key, value := range form {
			values.Set(key, value)
		}
		return values, nil
	case map[string][]string:
		return url.Values(form), nil
	default:
		return nil, fmt.Errorf("unsupported webhook payload type %T", payload)
	}
}

// signedMaterial builds the Twilio signature input: the full request URL
// followed by every form parameter (sorted by parameter name) concatenated
// as raw key+value pairs with no separators.
func signedMaterial(requestURL string, form url.Values) []byte {
	keys := make([]string, 0, len(form))
	for key := range form {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	buf := make([]byte, 0, len(requestURL)+2*len(keys))
	buf = append(buf, requestURL...)
	for _, key := range keys {
		buf = append(buf, key...)
		buf = append(buf, form.Get(key)...)
	}
	return buf
}
