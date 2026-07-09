package transfer

import "testing"

func TestPartSize(t *testing.T) {
	tests := []struct {
		name       string
		totalBytes int64
		want       int64
	}{
		// One representative value per table bucket.
		{"tiny file below 5MB (edge case, not expected in practice)", 1024 * 1024, minPartSize},
		{"50MB -> 5MB bucket", 50 * 1024 * 1024, minPartSize},
		{"500MB -> 16MB bucket", 500 * 1024 * 1024, defaultPartSize},
		{"5GB -> 64MB bucket", 5 * 1024 * 1024 * 1024, largePartSize},
		{"50GB -> 128MB bucket", 50 * 1024 * 1024 * 1024, maxPartSize},

		// Boundary values: each threshold belongs to the bucket above it
		// (PartSize uses "<", never "<=").
		{"exactly 100MB -> 16MB bucket, not 5MB", 100 * 1024 * 1024, defaultPartSize},
		{"just under 100MB -> 5MB bucket", 100*1024*1024 - 1, minPartSize},
		{"exactly 1GB -> 64MB bucket, not 16MB", 1024 * 1024 * 1024, largePartSize},
		{"just under 1GB -> 16MB bucket", 1024*1024*1024 - 1, defaultPartSize},
		{"exactly 10GB -> 128MB bucket, not 64MB", 10 * 1024 * 1024 * 1024, maxPartSize},
		{"just under 10GB -> 64MB bucket", 10*1024*1024*1024 - 1, largePartSize},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := PartSize(tt.totalBytes)
			if got != tt.want {
				t.Errorf("PartSize(%d) = %d, want %d", tt.totalBytes, got, tt.want)
			}
			if got <= 0 {
				t.Errorf("PartSize(%d) = %d, must be positive", tt.totalBytes, got)
			}
		})
	}
}

// TestPartSize_10000PartClamp verifies the protective clamp: for a file
// large enough that the table's 128MB part size would need more than
// 10000 parts (i.e. totalBytes > maxPartSize * maxPartsPerUpload, roughly
// 1.28TB), PartSize must return a larger part size so that the resulting
// part count fits within S3's 10000-part protocol limit.
func TestPartSize_10000PartClamp(t *testing.T) {
	const tableCeiling = maxPartSize * maxPartsPerUpload // ~1.28TB

	// Comfortably above the table ceiling, so the 128MB bucket alone
	// would require more than 10000 parts.
	totalBytes := int64(tableCeiling) + 10*1024*1024*1024 // +10GB

	got := PartSize(totalBytes)

	if got <= maxPartSize {
		t.Fatalf("PartSize(%d) = %d, want a part size larger than the table ceiling %d (clamp should have kicked in)", totalBytes, got, int64(maxPartSize))
	}

	if got%clampRoundingUnit != 0 {
		t.Errorf("PartSize(%d) = %d, want a multiple of clampRoundingUnit (%d)", totalBytes, got, int64(clampRoundingUnit))
	}

	count := PartCount(totalBytes, got)
	if count > maxPartsPerUpload {
		t.Errorf("PartCount(%d, %d) = %d, want <= %d (S3's protocol limit)", totalBytes, got, count, int64(maxPartsPerUpload))
	}
}

// TestPartSize_NoClampBelowLimit sanity-checks that the clamp does NOT
// alter the table's answer for a file just under the threshold where more
// than 10000 128MB parts would be needed.
func TestPartSize_NoClampBelowLimit(t *testing.T) {
	const tableCeiling = maxPartSize * maxPartsPerUpload // ~1.28TB

	totalBytes := int64(tableCeiling)

	got := PartSize(totalBytes)
	if got != maxPartSize {
		t.Errorf("PartSize(%d) = %d, want unclamped table value %d", totalBytes, got, int64(maxPartSize))
	}
}

func TestPartCount(t *testing.T) {
	tests := []struct {
		name       string
		totalBytes int64
		partSize   int64
		want       int64
	}{
		{"exact multiple, no extra part", 100, 10, 10},
		{"remainder rounds up", 101, 10, 11},
		{"single byte remainder", 21, 10, 3},
		{"partSize larger than totalBytes -> one part", 5, 10, 1},
		{"totalBytes zero -> zero parts", 0, 10, 0},
		{"totalBytes negative -> zero parts", -5, 10, 0},
		{"partSize zero -> zero parts (defensive)", 100, 0, 0},
		{"partSize negative -> zero parts (defensive)", 100, -1, 0},
		{"realistic: 5GB file at 64MB parts", 5 * 1024 * 1024 * 1024, largePartSize, 80},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := PartCount(tt.totalBytes, tt.partSize)
			if got != tt.want {
				t.Errorf("PartCount(%d, %d) = %d, want %d", tt.totalBytes, tt.partSize, got, tt.want)
			}
		})
	}
}
