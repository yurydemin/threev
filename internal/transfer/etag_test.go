package transfer

import (
	"crypto/md5" //nolint:gosec // test-only use, matching production code's rationale in etag.go
	"encoding/hex"
	"strings"
	"testing"
)

// md5Hex returns the hex-encoded MD5 digest of s, exactly the shape a real
// S3 part ETag takes (quotes already stripped) - used to build
// deterministic, realistic-looking part ETags for these tests.
func md5Hex(s string) string {
	sum := md5.Sum([]byte(s)) //nolint:gosec // test-only use
	return hex.EncodeToString(sum[:])
}

func TestComputeCompositeETag(t *testing.T) {
	t.Parallel()

	partETags := []string{md5Hex("part one contents"), md5Hex("part two contents"), md5Hex("part three contents")}

	// Compute the expected result independently, directly from the
	// documented algorithm (docs/02-tech-spec.md section 10.2:
	// hex(md5(concat(part_md5_bytes)))-N), rather than reusing
	// computeCompositeETag's own internals - so this test actually
	// verifies the formula, not just that the function is consistent
	// with itself.
	var concat []byte
	for _, etag := range partETags {
		digest, err := hex.DecodeString(etag)
		if err != nil {
			t.Fatalf("hex.DecodeString(%q) returned error: %v", etag, err)
		}
		concat = append(concat, digest...)
	}
	sum := md5.Sum(concat) //nolint:gosec // test-only use
	want := hex.EncodeToString(sum[:]) + "-3"

	got, err := computeCompositeETag(partETags)
	if err != nil {
		t.Fatalf("computeCompositeETag() returned error: %v", err)
	}
	if got != want {
		t.Errorf("computeCompositeETag() = %q, want %q", got, want)
	}
}

func TestComputeCompositeETagStripsQuotes(t *testing.T) {
	t.Parallel()

	bare := []string{md5Hex("a"), md5Hex("b")}
	quoted := []string{`"` + bare[0] + `"`, `"` + bare[1] + `"`}

	wantETag, err := computeCompositeETag(bare)
	if err != nil {
		t.Fatalf("computeCompositeETag(bare) returned error: %v", err)
	}

	got, err := computeCompositeETag(quoted)
	if err != nil {
		t.Fatalf("computeCompositeETag(quoted) returned error: %v", err)
	}

	if got != wantETag {
		t.Errorf("computeCompositeETag(quoted) = %q, want %q (quotes must be stripped before hex-decoding)", got, wantETag)
	}
}

func TestComputeCompositeETagInvalidHex(t *testing.T) {
	t.Parallel()

	_, err := computeCompositeETag([]string{"not-valid-hex!!"})
	if err == nil {
		t.Fatal("computeCompositeETag() returned nil error, want a hex-decode error")
	}
}

func TestComputeCompositeETagWrongDigestLength(t *testing.T) {
	t.Parallel()

	// Valid hex, but too short to be an MD5 digest (16 bytes / 32 hex
	// chars).
	_, err := computeCompositeETag([]string{"aabb"})
	if err == nil {
		t.Fatal("computeCompositeETag() returned nil error, want an error for a non-16-byte digest")
	}
}

func TestVerifyMultipartETagMatch(t *testing.T) {
	t.Parallel()

	partETags := []string{md5Hex("alpha"), md5Hex("beta")}

	expected, err := computeCompositeETag(partETags)
	if err != nil {
		t.Fatalf("computeCompositeETag() returned error: %v", err)
	}

	verified, err := verifyMultipartETag(expected, partETags)
	if err != nil {
		t.Fatalf("verifyMultipartETag() returned error: %v", err)
	}
	if !verified {
		t.Error("verifyMultipartETag() = false, want true for a matching composite ETag")
	}

	// Quoted, as CompleteMultipartUploadOutput.ETag would arrive before
	// multipart_upload.go strips it - verifyMultipartETag must strip it
	// itself too, so callers do not have to special-case this.
	verified, err = verifyMultipartETag(`"`+expected+`"`, partETags)
	if err != nil {
		t.Fatalf("verifyMultipartETag() (quoted) returned error: %v", err)
	}
	if !verified {
		t.Error("verifyMultipartETag() (quoted) = false, want true")
	}

	// Case-insensitive: an uppercase-hex composite ETag from a
	// non-AWS-but-still-standard-format provider must still verify.
	upper := strings.ToUpper(expected)
	verified, err = verifyMultipartETag(upper, partETags)
	if err != nil {
		t.Fatalf("verifyMultipartETag() (uppercase) returned error: %v", err)
	}
	if !verified {
		t.Error("verifyMultipartETag() (uppercase) = false, want true (comparison must be case-insensitive)")
	}
}

func TestVerifyMultipartETagMismatch(t *testing.T) {
	t.Parallel()

	partETags := []string{md5Hex("alpha"), md5Hex("beta")}

	// Well-formed composite ETag format, but not the one that actually
	// corresponds to partETags.
	wrongButWellFormed := md5Hex("something else entirely") + "-2"

	verified, err := verifyMultipartETag(wrongButWellFormed, partETags)
	if err != nil {
		t.Fatalf("verifyMultipartETag() returned error: %v", err)
	}
	if verified {
		t.Error("verifyMultipartETag() = true, want false for a non-matching composite ETag")
	}
}

// TestVerifyMultipartETagNonStandardFormatIsSkippedNotFailed covers the
// documented fallback (etag.go's verifyMultipartETag doc comment,
// docs/02-tech-spec.md section 10.2 / the Этап 3 plan's architecture
// decisions): an ETag that does not match the "32-hex-N" composite format
// at all - as SSE-KMS or other non-standard S3-compatible providers may
// return - must be treated as "verification not applicable"
// (verified=false, err=nil), never as an error.
func TestVerifyMultipartETagNonStandardFormatIsSkippedNotFailed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		etag string
	}{
		{"not hex at all", "this-is-not-a-standard-etag-format"},
		{"right length hex but missing the -N suffix", md5Hex("no suffix")},
		{"hex with wrong length (not 32 chars) plus suffix", "abcd-3"},
		{"contains non-hex/non-printable-looking characters", "zz112233zz112233zz112233zz11223-4"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			verified, err := verifyMultipartETag(tt.etag, []string{md5Hex("irrelevant")})
			if err != nil {
				t.Fatalf("verifyMultipartETag(%q) returned error %v, want nil (format mismatch must be a silent skip)", tt.etag, err)
			}
			if verified {
				t.Errorf("verifyMultipartETag(%q) = true, want false", tt.etag)
			}
		})
	}
}

func TestVerifySingleETag(t *testing.T) {
	t.Parallel()

	sum := md5.Sum([]byte("hello world")) //nolint:gosec // test-only use
	plain := hex.EncodeToString(sum[:])

	if !verifySingleETag(plain, sum) {
		t.Error("verifySingleETag() = false, want true for a matching plain MD5 ETag")
	}
	if !verifySingleETag(`"`+plain+`"`, sum) {
		t.Error("verifySingleETag() (quoted) = false, want true")
	}
	if verifySingleETag(plain, md5.Sum([]byte("different content"))) { //nolint:gosec // test-only use
		t.Error("verifySingleETag() = true, want false for a mismatched digest")
	}

	// A composite (multipart-source) ETag is a different format from a
	// plain single-part ETag - verifySingleETag must not attempt to
	// compare it at all, exactly as verifyMultipartETag skips
	// non-composite-format ETags.
	if verifySingleETag(plain+"-2", sum) {
		t.Error("verifySingleETag() = true for a composite-format ETag, want false (format not applicable)")
	}
}
