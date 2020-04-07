package mmf

import (
	"fmt"
	"log"
	"strconv"
	"time"

	"open-match.dev/open-match/pkg/matchfunction"
	"open-match.dev/open-match/pkg/pb"
)

// This match function fetches all the Tickets for all the pools specified in
// the profile. It uses a configured number of tickets from each pool to generate
// a Match Proposal. It continues to generate proposals till one of the pools
// runs out of Tickets.
const (
	matchName                 = "basic-matchfunction"
	mixTicketsPerPoolPerMatch = 2
	maxTicketsPerPoolPerMatch = 4
)

// Run is this match function's implementation of the gRPC call defined in api/matchfunction.proto.
func (s *MatchFunctionService) Run(req *pb.RunRequest, stream pb.MatchFunction_RunServer) error {
	// Fetch tickets for the pools specified in the Match Profile.
	log.Printf("Generating proposals for function %v", req.GetProfile().GetName())

	for _, pool := range req.GetProfile().GetPools() {
		// Get Player Tickets.
		playerPool := *pool
		playerTag := pb.TagPresentFilter{Tag: "player"}
		playerPool.TagPresentFilters = append(playerPool.TagPresentFilters, &playerTag)
		playerTickets, err := matchfunction.QueryPool(stream.Context(), s.queryServiceClient, &playerPool)
		if err != nil {
			log.Printf("Failed to query tickets for the given pool, got %s", err.Error())
			return err
		}

		// Get Backfill Ticket.
		backfillPool := *pool
		backfillTag := pb.TagPresentFilter{Tag: "backfill"}
		backfillPool.TagPresentFilters = append(backfillPool.TagPresentFilters, &backfillTag)
		backfillTickets, err := matchfunction.QueryPool(stream.Context(), s.queryServiceClient, &backfillPool)
		if err != nil {
			log.Printf("Failed to query tickets for the given pool, got %s", err.Error())
			return err
		}

		// Generate proposal.
		proposals, err := makeMatches(req.GetProfile(), playerTickets, backfillTickets)
		if err != nil {
			log.Printf("Failed to generate matches, got %s", err.Error())
			return err
		}

		log.Printf("Streaming %v proposals to Open Match", len(proposals))

		// Stream the generated proposals back to Open Match.
		for _, proposal := range proposals {
			if err := stream.Send(&pb.RunResponse{Proposal: proposal}); err != nil {
				log.Printf("Failed to stream proposals to Open Match, got %s", err.Error())
				return err
			}
		}
	}

	return nil
}

// makeMatches Matcheを作成
func makeMatches(p *pb.MatchProfile, playerTickets []*pb.Ticket, backfillTickets []*pb.Ticket) ([]*pb.Match, error) {
	var matches []*pb.Match

	matchTickets := []*pb.Ticket{}

	// BackFillチケットから空いているプレイヤーを埋めていく
	for _, backfillTicket := range backfillTickets {
		// 現在のプレイヤー数を取得
		extensions := backfillTicket.GetAssignment().GetExtensions()
		joinablePlayerNumByte := extensions["joinablePlayerNum"].GetValue()
		joinablePlayerNumStr := string(joinablePlayerNumByte)
		joinablePlayerNum, err := strconv.Atoi(joinablePlayerNumStr)
		if err != nil {
			return matches, err
		}

		// 不足しているプレイヤー数分のPlayerTicketを1Matcheにまとめる
		var joinPalyerNum int
		if len(playerTickets) <= joinablePlayerNum {
			joinPalyerNum = len(playerTickets)
		} else {
			joinPalyerNum = joinablePlayerNum
		}
		if joinPalyerNum <= 0 {
			continue
		}
		matchTickets = append(matchTickets, backfillTicket)
		matchTickets = append(matchTickets, playerTickets[0:joinPalyerNum]...)
		playerTickets = playerTickets[joinPalyerNum:]

		matches = append(matches, &pb.Match{
			MatchId:       fmt.Sprintf("profile-%v-time-%v", p.GetName(), time.Now().Format("2006-01-02T15:04:05.00")),
			MatchProfile:  p.GetName(),
			MatchFunction: matchName,
			Tickets:       matchTickets,
		})
	}

	// 通常のマッチメイク
	if len(playerTickets) <= 0 {
		return matches, nil
	}

	for {
		if len(playerTickets) < mixTicketsPerPoolPerMatch {
			break
		}

		if maxTicketsPerPoolPerMatch <= len(playerTickets) {
			matchTickets = append(matchTickets, playerTickets[0:maxTicketsPerPoolPerMatch]...)
			playerTickets = playerTickets[maxTicketsPerPoolPerMatch:]
		} else {
			currentTickets := len(playerTickets)
			matchTickets = append(matchTickets, playerTickets[0:currentTickets]...)
			playerTickets = playerTickets[currentTickets:]
		}
		matches = append(matches, &pb.Match{
			MatchId:       fmt.Sprintf("profile-%v-time-%v", p.GetName(), time.Now().Format("2006-01-02T15:04:05.00")),
			MatchProfile:  p.GetName(),
			MatchFunction: matchName,
			Tickets:       matchTickets,
		})
	}

	return matches, nil
}
