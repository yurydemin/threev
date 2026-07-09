package transfer

import (
	"crypto/md5" //nolint:gosec // MD5 is required to match S3's ETag/composite-ETag format (docs/02-tech-spec.md section 10.2, FR-TR-004), not used for any cryptographic/security purpose - identical rationale to the InsecureSkipVerify precedent elsewhere in this codebase.
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
)

// compositeETagPattern matches the S3 multipart-upload composite ETag
// format: 32 hex characters (the MD5 of the concatenated per-part MD5
// digests) followed by "-" and the part count. It accepts both letter
// cases in the hex portion (S3 itself always returns lowercase, but the
// match is deliberately case-insensitive so a well-formed-but-uppercase
// ETag from some other S3-compatible provider is still recognized as this
// format rather than rejected outright); verifyMultipartETag compares
// values case-insensitively too, for the same reason.
var compositeETagPattern = regexp.MustCompile(`^[0-9a-fA-F]{32}-\d+$`)

// plainETagPattern matches a plain, non-multipart S3 ETag: a bare 32-hex
// MD5 digest, with no "-N" suffix. Used by verifySingleETag to recognize
// when a PutObject response's ETag is directly comparable against a
// locally computed MD5.
var plainETagPattern = regexp.MustCompile(`^[0-9a-fA-F]{32}$`)

// computeCompositeETag computes the S3 multipart-upload composite ETag for
// a sequence of already-uploaded parts, given their individual ETags in
// ascending PartNumber order (partETags[0] is part 1, partETags[1] is part
// 2, and so on - the caller, multipart_upload.go, is responsible for
// ordering this slice correctly; computeCompositeETag itself has no
// concept of part numbers).
//
// Each element of partETags is expected to already be the "bare" per-part
// MD5 hex digest S3 returns in UploadPartOutput.ETag/types.Part.ETag, with
// its surrounding double quotes stripped - callers must strip them before
// calling this function, exactly as they must before comparing an ETag
// against anything else.
//
// The algorithm (docs/02-tech-spec.md section 10.2, FR-TR-004:
// "составной ETag (md5(parts))"), matching what S3 itself computes:
//  1. hex-decode each part's ETag back into its raw 16-byte MD5 digest;
//  2. concatenate all part digests, in part-number order;
//  3. MD5 that concatenation;
//  4. hex-encode the result and append "-{len(partETags)}".
func computeCompositeETag(partETags []string) (string, error) {
	concat := make([]byte, 0, len(partETags)*md5.Size)

	for i, etag := range partETags {
		clean := strings.Trim(etag, `"`)

		digest, err := hex.DecodeString(clean)
		if err != nil {
			return "", fmt.Errorf("part %d: decode ETag %q as hex: %w", i+1, etag, err)
		}

		if len(digest) != md5.Size {
			return "", fmt.Errorf("part %d: ETag %q decodes to %d bytes, want %d (not a plain MD5 digest)", i+1, etag, len(digest), md5.Size)
		}

		concat = append(concat, digest...)
	}

	sum := md5.Sum(concat) //nolint:gosec // see package-level rationale above

	return fmt.Sprintf("%s-%d", hex.EncodeToString(sum[:]), len(partETags)), nil
}

// verifyMultipartETag reports whether actualETag (already stripped of its
// surrounding quotes, as returned by CompleteMultipartUploadOutput.ETag)
// matches the composite ETag computed from partETags (in ascending
// PartNumber order - see computeCompositeETag).
//
// If actualETag does not look like a composite multipart ETag
// (compositeETagPattern) at all, verification is skipped rather than
// treated as a failure: verified=false, err=nil. This is a deliberate,
// documented fallback (docs/02-tech-spec.md section 10.2 and the Этап 3
// plan's architecture decisions), not an oversight: S3-compatible
// providers using server-side encryption (SSE-KMS) or otherwise deviating
// from AWS's standard ETag format return an ETag this function has no way
// to independently recompute. FR-TR-004's integrity check is a
// best-effort verification layered on top of the HTTP-level success of
// CompleteMultipartUpload, not the sole source of truth for whether the
// upload succeeded.
//
// A non-nil error return is reserved for computeCompositeETag itself
// failing on malformed partETags input (a programming/data-integrity error
// in the caller - e.g. a part ETag that was never actually a plain MD5
// digest), not an expected runtime condition during normal operation.
func verifyMultipartETag(actualETag string, partETags []string) (verified bool, err error) {
	clean := strings.Trim(actualETag, `"`)

	if !compositeETagPattern.MatchString(clean) {
		return false, nil
	}

	expected, err := computeCompositeETag(partETags)
	if err != nil {
		return false, fmt.Errorf("compute expected composite ETag: %w", err)
	}

	return strings.EqualFold(clean, expected), nil
}

// verifySingleETag reports whether actualETag (already stripped of
// quotes) is a plain (non-multipart) MD5-format ETag matching computedMD5,
// the MD5 digest computed locally while streaming a file to PutObject
// (upload.go). Like verifyMultipartETag, a non-matching format (an
// SSE-KMS or otherwise non-standard provider ETag) is not distinguished
// from a genuine digest mismatch: both simply mean "not verified", which
// upload.go treats identically - see its doc comment for why a
// verification failure is not surfaced as a fatal error in this MVP.
func verifySingleETag(actualETag string, computedMD5 [md5.Size]byte) (verified bool) {
	clean := strings.Trim(actualETag, `"`)
	if !plainETagPattern.MatchString(clean) {
		return false
	}

	return strings.EqualFold(clean, hex.EncodeToString(computedMD5[:]))
}
