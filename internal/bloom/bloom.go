package bloom

type Filter []byte

func hash64(data []byte) uint64 {
	const (
		offset64 = 14695981039346656037
		prime64  = 1099511628211
	)
	h := uint64(offset64)
	for _, b := range data {
		h ^= uint64(b)
		h *= prime64
	}
	// independent and well-distributed halves
	h ^= h >> 33
	h *= 0xff51afd7ed558ccd
	h ^= h >> 33
	h *= 0xc4ceb9fe1a85ec53
	h ^= h >> 33
	return h
}

func New(keys [][]byte, bitsPerKey int) Filter {
	if len(keys) == 0 || bitsPerKey <= 0 {
		return nil
	}

	k := int(float64(bitsPerKey) * 0.69)
	if k < 1 {
		k = 1
	}
	if k > 30 {
		k = 30
	}

	bits := max(len(keys)*bitsPerKey, 64)
	numBytes := (bits + 7) / 8
	bits = numBytes * 8

	f := make(Filter, numBytes+1)
	f[numBytes] = byte(k)

	for _, key := range keys {
		h := hash64(key)
		h1 := uint32(h)
		h2 := uint32(h >> 32)
		for i := range k {
			bitPos := (h1 + uint32(i)*h2) % uint32(bits)
			f[bitPos/8] |= 1 << (bitPos % 8)
		}
	}
	return f
}

func (f Filter) MayContain(key []byte) bool {
	if len(f) < 2 {
		return true
	}
	numBytes := len(f) - 1
	k := int(f[numBytes])
	if k == 0 || k > 30 {
		return true
	}
	bits := uint32(numBytes * 8)

	h := hash64(key)
	h1 := uint32(h)
	h2 := uint32(h >> 32)
	for i := range k {
		bitPos := (h1 + uint32(i)*h2) % bits
		if (f[bitPos/8] & (1 << (bitPos % 8))) == 0 {
			return false
		}
	}
	return true
}
