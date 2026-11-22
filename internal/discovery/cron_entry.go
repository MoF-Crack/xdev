package discovery

import "strings"

// managedSniffCron recognizes only an explicit ownership marker, or the exact
// historical command for this executable. It never interprets shell code.
func managedSniffCron(line, exe string) (string, bool) {
	remaining := strings.TrimLeft(line, " \t")
	if remaining == "" || strings.HasPrefix(remaining, "#") {
		return "", false
	}
	fields := make([]string, 0, 5)
	for i := 0; i < 5; i++ {
		end := strings.IndexAny(remaining, " \t")
		if end < 0 {
			return "", false
		}
		fields = append(fields, remaining[:end])
		remaining = strings.TrimLeft(remaining[end:], " \t")
	}
	schedule := strings.Join(fields, " ")
	if parseCronSchedule(schedule) != nil {
		return "", false
	}
	words, comment, ok := literalCronWords(remaining)
	if !ok || len(words) == 0 {
		return "", false
	}
	if comment == sniffCronMarker {
		return schedule, true
	}
	if comment != "" || len(words) != 4 || words[1] != "sniff" || words[2] != "--all" || words[3] != "--headless" {
		return "", false
	}
	// An arbitrary other /path/xdev is not proof of historical ownership.
	if words[0] == exe {
		return schedule, true
	}
	return "", false
}

// literalCronWords accepts literal shell words and POSIX quote concatenation
// (including '\” emitted for apostrophes). Expansions, redirections, command
// lists, cron stdin (%), and malformed quoting are deliberately unrecognized.
func literalCronWords(command string) ([]string, string, bool) {
	var words []string
	var word strings.Builder
	var quote byte
	started := false
	for i := 0; i < len(command); i++ {
		c := command[i]
		// Cron handles this escape before the shell, even inside quotes.
		if c == '\\' && i+1 < len(command) && command[i+1] == '%' {
			word.WriteByte('%')
			started = true
			i++
			continue
		}
		if c == '%' || c == '\n' || c == '\r' || c == 0 {
			return nil, "", false
		}
		if quote == '\'' {
			if c == '\'' {
				quote = 0
			} else {
				word.WriteByte(c)
			}
			continue
		}
		if c == '\\' {
			if i+1 >= len(command) {
				return nil, "", false
			}
			i++
			word.WriteByte(command[i])
			started = true
			continue
		}
		if quote == '"' {
			if c == '"' {
				quote = 0
			} else if c == '$' || c == '`' {
				return nil, "", false
			} else {
				word.WriteByte(c)
			}
			continue
		}
		switch c {
		case '\'', '"':
			quote = c
			started = true
		case ' ', '\t':
			if started {
				words = append(words, word.String())
				word.Reset()
				started = false
			}
		case '#':
			if started {
				return nil, "", false
			}
			return words, strings.TrimSpace(command[i+1:]), true
		case '$', '`', ';', '&', '|', '<', '>', '(', ')', '*', '?', '[', ']':
			return nil, "", false
		default:
			word.WriteByte(c)
			started = true
		}
	}
	if quote != 0 {
		return nil, "", false
	}
	if started {
		words = append(words, word.String())
	}
	return words, "", true
}
