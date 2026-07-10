// Positive-control fixture for authority_uncertainty_primaryip_ambiguous.
// node.PrimaryIP() in an identity-critical context.
package badfix

type node struct{}

func (n *node) PrimaryIP() string { return "10.0.0.1" }

func register(n *node) string {
	ip := n.PrimaryIP() // BAD: returns floating VIP when held
	return ip
}
