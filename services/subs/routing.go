package subs

import "github.com/ITG-Labs/sylph/pkg/clientroute"

func routingHeader(view *View, conns []Connection, userAgent string) (string, error) {
	if view.ClientRouting == nil || len(view.ClientRouting.Rules) == 0 {
		return "", nil
	}
	client, ok := clientroute.DetectClient(userAgent)
	if !ok {
		return "", nil
	}
	in := clientroute.Input{Title: brandTitle(view), UpdatedAt: view.ClientRouting.UpdatedAt}
	for _, r := range view.ClientRouting.Rules {
		res := clientroute.Resolved{Rule: r}
		if r.Action == clientroute.ActionNode {
			for _, c := range conns {
				if c.NodeID == r.NodeID {
					res.ServerName = c.Name
					break
				}
			}
		}
		in.Rules = append(in.Rules, res)
	}
	link, _, err := clientroute.Render(client, in)
	return link, err
}
