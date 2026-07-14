# Source Evidence: supported rule parser cases

> Source file: `rules/parser.go`
> Reason: public configuration evidence for rule type names.
> Commit: `a84724665eb7f989809abe463c05f5723bd24975`

`ParseRule` contains explicit cases for these rule types:

```text
DOMAIN, DOMAIN-SUFFIX, DOMAIN-KEYWORD, DOMAIN-REGEX, DOMAIN-WILDCARD,
GEOSITE, GEOIP, SRC-GEOIP, IP-ASN, SRC-IP-ASN, IP-CIDR, IP-CIDR6,
SRC-IP-CIDR, IP-SUFFIX, SRC-IP-SUFFIX, SRC-PORT, DST-PORT, IN-PORT,
DSCP, PROCESS-NAME, PROCESS-PATH, PROCESS-NAME-REGEX,
PROCESS-PATH-REGEX, PROCESS-NAME-WILDCARD, PROCESS-PATH-WILDCARD,
NETWORK, UID, IN-TYPE, IN-USER, IN-NAME, SUB-RULE, AND, OR, NOT,
RULE-SET, MATCH
```

The parser rejects an empty payload for all types except `MATCH`, returns an
error for unknown types, and delegates the logical and provider-backed cases to
their respective packages.
