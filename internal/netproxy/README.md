# 2Ag local proxy

The proxy is a loopback forward proxy used by the launcher. It can block
configured hostnames, route plain HTTP requests to configured upstream URLs,
apply custom headers, and prepend `global_rules` to an explicitly configured
JSON field.

`CONNECT` is intentionally opaque. 2Ag forwards the TCP tunnel and does not
install a CA certificate or decrypt TLS. As a result, HTTPS request bodies
cannot be rewritten by this module.
