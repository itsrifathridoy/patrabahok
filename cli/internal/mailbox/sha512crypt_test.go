package mailbox

import "testing"

// Vectors from the SHA-crypt specification
// (https://www.akkadia.org/drepper/SHA-crypt.txt), plus long/empty inputs.
func TestSHA512Crypt(t *testing.T) {
	cases := []struct {
		password, salt string
		rounds         int
		want           string
	}{
		{"Hello world!", "saltstring", 5000,
			"$6$saltstring$svn8UoSVapNtMuq1ukKS4tPQd8iKwSMHWjl/O817G3uBnIFNjnQJuesI68u4OTLiBFdcbYEdFCoEOfaS35inz1"},
		{"Hello world!", "saltstringsaltstring", 10000,
			"$6$rounds=10000$saltstringsaltst$OW1/O6BYHV6BcXZu8QVeXbDWra3Oeqh0sbHbbMCVNSnCM/UrjmM0Dp8vOuZeHBy/YTBmSK6H9qs/y3RnOaw5v."},
		{"This is just a test", "toolongsaltstring", 5000,
			"$6$toolongsaltstrin$lQ8jolhgVRVhY4b5pZKaysCLi0QBxGoNeKQzQ3glMhwllF7oGDZxUhx1yxdYcz/e1JSbq3y6JMxxl8audkUEm0"},
		{"a very much longer text to encrypt.  This one even stretches over morethan one line.", "anotherlongsaltstring", 1400,
			"$6$rounds=1400$anotherlongsalts$POfYwTEok97VWcjxIiSOjiykti.o/pQs.wPvMxQ6Fm7I6IoYN3CmLs66x9t0oSwbtEW7o7UmJEiDwGqd8p4ur1"},
	}
	for _, c := range cases {
		if got := sha512Crypt(c.password, c.salt, c.rounds); got != c.want {
			t.Errorf("sha512Crypt(%q, %q, %d)\n got  %s\n want %s", c.password, c.salt, c.rounds, got, c.want)
		}
	}
}

func TestValidateLocalPart(t *testing.T) {
	good := []string{"student1", "first.last", "a_b-c", "x+tag", "o'brien"}
	bad := []string{"", "../etc", "a/b", "..", ".a", "a.", "a..b", `a\b`, "a b", "a@b"}
	for _, l := range good {
		if err := ValidateEmail(l + "@example.com"); err != nil {
			t.Errorf("%q rejected: %v", l, err)
		}
	}
	for _, l := range bad {
		if err := ValidateEmail(l + "@example.com"); err == nil {
			t.Errorf("%q accepted", l)
		}
	}
}
