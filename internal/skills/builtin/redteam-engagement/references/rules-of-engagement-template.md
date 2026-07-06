# Rules of Engagement Template

Fill this in with the user and confirm it back to them before any tool call
that reaches the network (`recon_scan`, `dast_scan`). Keep it short — this is
a scope confirmation, not a contract.

## Authorized targets

List every host, IP, CIDR range, or URL in scope. Be literal — "my home lab"
is not a target list; `192.168.1.0/24` and `nas.lan` are.

- `<target 1>`
- `<target 2>`

## Explicitly out of bounds

Anything adjacent that must NOT be touched even if discovered during a scan
(e.g. a router's WAN-side IP, a neighbor's device on a shared subnet, a
production system on the same network as a home-lab target).

- `<excluded target/system>`

## Testing depth

- [ ] Passive/baseline only (recon_scan default, dast_scan baseline mode)
- [ ] Active testing authorized (`security.dast.allow_active: true` is set)
      — nmap OS detection/full port range, nuclei's full template set,
      dast_scan active/api mode

## Time window

When is testing authorized to run? (e.g. "now, one-time", "this session
only", "ongoing until told otherwise")

## Notes

Anything else worth recording before starting — fragile devices on the
network, expected false positives, prior known issues already accepted.
