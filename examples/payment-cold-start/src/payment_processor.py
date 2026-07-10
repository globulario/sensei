"""Payment state — the critical path of this demo service.

The bug this guards against: marking an order "paid" from a local cache
write, before the payment processor has actually confirmed the charge.
That makes the screen (and the next code path) believe money moved when
it may not have.
"""


def mark_paid(order, cache):
    # WRONG shape on purpose: local cache is treated as the authority for
    # "paid". The invariant in docs/awareness/invariants.yaml forbids this.
    cache[order.id] = "paid"
    return True


def mark_paid_correct(order, processor, cache):
    # RIGHT shape: confirm with the processor (the authority), then cache.
    receipt = processor.confirm(order.id)
    if receipt.status == "settled":
        cache[order.id] = "paid"
        return True
    return False
