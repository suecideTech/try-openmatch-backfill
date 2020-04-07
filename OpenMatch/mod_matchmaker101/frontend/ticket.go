package main

import (
	any "github.com/golang/protobuf/ptypes/any"
	"open-match.dev/open-match/pkg/pb"
)

// Ticket generates a Ticket with a mode search field that has one of the
// randomly selected modes.
func makeTicket(gamemode string) *pb.Ticket {
	ticket := &pb.Ticket{
		SearchFields: &pb.SearchFields{
			// Tags can support multiple values but for simplicity, the demo function
			// assumes only single mode selection per Ticket.
			Tags: []string{
				gamemode,
				"player",
			},
		},
	}

	return ticket
}

func makeBackfillTicket(gamemode string, connection string, joinablePlayerNum string) *pb.Ticket {
	var anyJoinablePlayerNum any.Any
	anyJoinablePlayerNum.Value = []byte(joinablePlayerNum)

	ticket := &pb.Ticket{
		SearchFields: &pb.SearchFields{
			Tags: []string{
				gamemode,
				"backfill",
			},
		},
		Assignment: &pb.Assignment{
			Connection: connection,
			Extensions: map[string]*any.Any{
				"joinablePlayerNum": &anyJoinablePlayerNum,
			},
		},
	}

	return ticket
}
