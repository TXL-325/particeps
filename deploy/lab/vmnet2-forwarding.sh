#!/bin/sh
# Only for the documented VMnet2 lab. This is not a production firewall policy.
set -eu

mode=${1:-add}
case "$mode" in add|remove) ;; *) echo 'usage: vmnet2-forwarding.sh [add|remove]' >&2; exit 2 ;; esac

rule() {
    if [ "$mode" = remove ]; then
        if iptables -w -C DOCKER-USER "$@" 2>/dev/null; then
            iptables -w -D DOCKER-USER "$@"
        fi
    elif ! iptables -w -C DOCKER-USER "$@" 2>/dev/null; then
        iptables -w -A DOCKER-USER "$@"
    fi
}

for ingress in 10.90.0.11 10.90.0.12; do
    rule -i ens37 -o particepsbr0 -d 10.80.0.0/24 \
        -m conntrack --ctstate DNAT --ctdir ORIGINAL --ctorigdst "$ingress" \
        -m comment --comment particeps-vmnet2-lab -j ACCEPT
    rule -i particepsbr0 -o ens37 -s 10.80.0.0/24 \
        -m conntrack --ctstate ESTABLISHED,RELATED --ctdir REPLY --ctorigdst "$ingress" \
        -m comment --comment particeps-vmnet2-lab -j ACCEPT
done
