package mailbox

import (
	"crypto/rand"
	"crypto/sha512"
	"strconv"
	"strings"
)

// sha512Crypt implements the SHA-512 crypt(3) scheme ("$6$", Ulrich Drepper's
// specification, https://www.akkadia.org/drepper/SHA-crypt.txt), the format Dovecot's
// SHA512-CRYPT scheme verifies. Hashing in-process replaces running
// `doveadm pw -p <password>`, which put every password on a command line where any
// local user could read it from the process list.
func sha512Crypt(password, salt string, rounds int) string {
	if len(salt) > 16 {
		salt = salt[:16]
	}
	p, s := []byte(password), []byte(salt)

	b := sha512.New()
	b.Write(p)
	b.Write(s)
	b.Write(p)
	sumB := b.Sum(nil)

	a := sha512.New()
	a.Write(p)
	a.Write(s)
	for i := len(p); i > 0; i -= 64 {
		if i > 64 {
			a.Write(sumB)
		} else {
			a.Write(sumB[:i])
		}
	}
	for i := len(p); i > 0; i >>= 1 {
		if i&1 != 0 {
			a.Write(sumB)
		} else {
			a.Write(p)
		}
	}
	sumA := a.Sum(nil)

	dp := sha512.New()
	for range p {
		dp.Write(p)
	}
	pSeq := repeatTo(dp.Sum(nil), len(p))

	ds := sha512.New()
	for i := 0; i < 16+int(sumA[0]); i++ {
		ds.Write(s)
	}
	sSeq := repeatTo(ds.Sum(nil), len(s))

	c := sumA
	for i := 0; i < rounds; i++ {
		h := sha512.New()
		if i&1 != 0 {
			h.Write(pSeq)
		} else {
			h.Write(c)
		}
		if i%3 != 0 {
			h.Write(sSeq)
		}
		if i%7 != 0 {
			h.Write(pSeq)
		}
		if i&1 != 0 {
			h.Write(c)
		} else {
			h.Write(pSeq)
		}
		c = h.Sum(nil)
	}

	var out strings.Builder
	out.WriteString("$6$")
	if rounds != 5000 {
		out.WriteString("rounds=" + strconv.Itoa(rounds) + "$")
	}
	out.WriteString(salt)
	out.WriteByte('$')
	order := [21][3]int{
		{0, 21, 42}, {22, 43, 1}, {44, 2, 23}, {3, 24, 45}, {25, 46, 4}, {47, 5, 26}, {6, 27, 48},
		{28, 49, 7}, {50, 8, 29}, {9, 30, 51}, {31, 52, 10}, {53, 11, 32}, {12, 33, 54}, {34, 55, 13},
		{56, 14, 35}, {15, 36, 57}, {37, 58, 16}, {59, 17, 38}, {18, 39, 60}, {40, 61, 19}, {62, 20, 41},
	}
	for _, o := range order {
		b64From24(&out, c[o[0]], c[o[1]], c[o[2]], 4)
	}
	b64From24(&out, 0, 0, c[63], 2)
	return out.String()
}

const cryptAlphabet = "./0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

func b64From24(out *strings.Builder, b2, b1, b0 byte, n int) {
	w := uint(b2)<<16 | uint(b1)<<8 | uint(b0)
	for ; n > 0; n-- {
		out.WriteByte(cryptAlphabet[w&0x3f])
		w >>= 6
	}
}

func repeatTo(digest []byte, n int) []byte {
	out := make([]byte, 0, n)
	for len(out) < n {
		need := n - len(out)
		if need > len(digest) {
			need = len(digest)
		}
		out = append(out, digest[:need]...)
	}
	return out
}

func randomSalt() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	for i := range b {
		b[i] = cryptAlphabet[int(b[i])%len(cryptAlphabet)]
	}
	return string(b), nil
}
