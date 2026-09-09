/*
 * generated-c-transform: observation transform.
 * Reads one JSON object on stdin, writes one JSON object on stdout, exits 0.
 * ISO C; only stdio.h and string.h. No files, network, fork, or env access.
 *
 * The scanner walks raw JSON text: a '"' token followed by ':' is a key, and
 * the value span (string spans kept verbatim, quotes included) is captured.
 * Escaped quotes are honored, and value-string contents are never tokenized,
 * so embedded text like "\"reject\":true" cannot spoof a real key.
 */
#include <stdio.h>
#include <string.h>

#define BUF_CAP ((size_t)(1u << 21)) /* > 1 MiB input limit from manifest */

static char buf[BUF_CAP];
static long len;

static long skip_ws(long i)
{
    while (i < len) {
        char c = buf[i];
        if (c == ' ' || c == '\t' || c == '\n' || c == '\r')
            i++;
        else
            break;
    }
    return i;
}

/* i points at an opening quote; returns index just past the closing quote. */
static long scan_string(long i)
{
    i++;
    while (i < len) {
        if (buf[i] == '\\') {
            i += 2;
            continue;
        }
        if (buf[i] == '"')
            return i + 1;
        i++;
    }
    return len;
}

/* Key contents span [s, e); compare against name. */
static int key_is(long s, long e, const char *name)
{
    size_t n = strlen(name);
    return (e - s) == (long)n && memcmp(buf + s, name, n) == 0;
}

/* End (exclusive) of a bare token value starting at i. */
static long bare_end(long i)
{
    while (i < len) {
        char c = buf[i];
        if (c == ',' || c == '}' || c == ']' ||
            c == ' ' || c == '\t' || c == '\n' || c == '\r')
            break;
        i++;
    }
    return i;
}

int main(void)
{
    long i, ts = -1, te = -1, ns = -1, ne = -1;
    int reject = 0;

    {
        size_t got;
        while (len < (long)BUF_CAP - 1 &&
               (got = fread(buf + len, 1, BUF_CAP - 1 - (size_t)len, stdin)) > 0)
            len += (long)got;
    }
    buf[len] = '\0';

    i = 0;
    while (i < len) {
        long qs, qe, vs, ve;
        if (buf[i] != '"') {
            i++;
            continue;
        }
        qs = i + 1;
        qe = scan_string(i); /* contents: [qs, qe-1) */
        i = skip_ws(qe);
        if (i >= len || buf[i] != ':')
            continue; /* string in value position, not a key */
        i = skip_ws(i + 1);
        vs = i;
        if (i < len && buf[i] == '"')
            ve = scan_string(i);
        else
            ve = bare_end(i);

        if (key_is(qs, qe - 1, "reject")) {
            if (ve - vs == 4 && memcmp(buf + vs, "true", 4) == 0)
                reject = 1;
        } else if (key_is(qs, qe - 1, "title")) {
            if (ts < 0 && vs < ve && buf[vs] == '"') {
                ts = vs;
                te = ve;
            }
        } else if (key_is(qs, qe - 1, "native_status")) {
            if (ns < 0 && vs < ve) {
                ns = vs;
                ne = ve;
            }
        }
        i = ve;
    }

    if (reject) {
        fputs("{\"error\":\"rejected\"}", stdout);
        return 0;
    }

    fputs("{\"facts\":{\"reader\":\"generated-c-transform\",\"has_snapshot\":true",
          stdout);
    if (ts >= 0) {
        fputs(",\"title\":", stdout);
        fwrite(buf + ts, 1, (size_t)(te - ts), stdout);
    }
    if (ns >= 0) {
        fputs(",\"native_status\":", stdout);
        fwrite(buf + ns, 1, (size_t)(ne - ns), stdout);
    }
    fputs("},\"uncertainty\":[],\"evidence\":[]}", stdout);
    return 0;
}
