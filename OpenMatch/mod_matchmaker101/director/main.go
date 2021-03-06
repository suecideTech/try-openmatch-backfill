package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/golang/protobuf/ptypes/any"
	"google.golang.org/grpc"
	"open-match.dev/open-match/pkg/pb"
)

type AllocatePort struct {
	Name string `json:"name"`
	Port int    `json:"port"`
}
type AllocateStatus struct {
	State          string         `json:"state"`
	GameServerName string         `json:"gameServerName"`
	Ports          []AllocatePort `json:"ports"`
	Address        string         `json:"address"`
	NodeName       string         `json:"nodeName"`
}
type AllocateResponce struct {
	Status AllocateStatus `json:"status"`
}

// The Director in this tutorial continously polls Open Match for the Match
// Profiles and makes random assignments for the Tickets in the returned matches.

const (
	// The endpoint for the Open Match Frontend service.
	omFrontendEndpoint = "om-frontend.open-match.svc.cluster.local:50504"

	// The endpoint for the Open Match Backend service.
	omBackendEndpoint = "om-backend.open-match.svc.cluster.local:50505"
	// The Host and Port for the Match Function service endpoint.
	functionHostName       = "matchfunction.openmatch.svc.cluster.local"
	functionPort     int32 = 50502

	// The Host and Port for the AllocateService endpoint.
	allocateHostName = "http://fleet-allocator-endpoint.default.svc.cluster.local/address"
	allocateKey      = "v1GameClientKey"
	allocatePass     = "EAEC945C371B2EC361DE399C2F11E"
)

var fe pb.FrontendServiceClient

func main() {
	// Connect to Open Match Backend.
	beConn, err := grpc.Dial(omBackendEndpoint, grpc.WithInsecure())
	if err != nil {
		log.Fatalf("Failed to connect to Open Match Backend, got %s", err.Error())
	}

	defer beConn.Close()
	be := pb.NewBackendServiceClient(beConn)

	// Connect to Open Match Frontend.
	feConn, err := grpc.Dial(omFrontendEndpoint, grpc.WithInsecure())
	if err != nil {
		log.Fatalf("Failed to connect to Open Match, got %v", err)
	}

	defer feConn.Close()
	fe = pb.NewFrontendServiceClient(feConn)

	// Generate the profiles to fetch matches for.
	profiles := generateProfiles()
	log.Printf("Fetching matches for %v profiles", len(profiles))

	for range time.Tick(time.Second * 1) {
		// Fetch matches for each profile and make random assignments for Tickets in
		// the matches returned.
		var wg sync.WaitGroup
		for _, p := range profiles {
			wg.Add(1)
			go func(wg *sync.WaitGroup, p *pb.MatchProfile) {
				defer wg.Done()
				matches, err := fetch(be, p)
				if err != nil {
					log.Printf("Failed to fetch matches for profile %v, got %s", p.GetName(), err.Error())
					return
				}

				if len(matches) > 0 {
					log.Printf("Generated %v matches for profile %v", len(matches), p.GetName())
				}
				if err := assign(be, matches); err != nil {
					log.Printf("Failed to assign servers to matches, got %s", err.Error())
					return
				}
			}(&wg, p)
		}

		wg.Wait()
	}
}

func fetch(be pb.BackendServiceClient, p *pb.MatchProfile) ([]*pb.Match, error) {
	req := &pb.FetchMatchesRequest{
		Config: &pb.FunctionConfig{
			Host: functionHostName,
			Port: functionPort,
			Type: pb.FunctionConfig_GRPC,
		},
		Profile: p,
	}

	stream, err := be.FetchMatches(context.Background(), req)
	if err != nil {
		log.Println()
		return nil, err
	}

	var result []*pb.Match
	for {
		resp, err := stream.Recv()
		if err == io.EOF {
			break
		}

		if err != nil {
			return nil, err
		}

		result = append(result, resp.GetMatch())
	}

	return result, nil
}

func assign(be pb.BackendServiceClient, matches []*pb.Match) error {
	for _, match := range matches {

		// BackFillTicketを含むMatchかチェック
		var backfillTicket *pb.Ticket = nil
		for _, t := range match.GetTickets() {
			tags := t.GetSearchFields().GetTags()
			for _, tag := range tags {
				if tag == "backfill" {
					//BackFillを含んでいる
					backfillTicket = t
					break
				}
			}
		}
		if backfillTicket != nil {
			err := backfillAssign(be, match, backfillTicket)
			if err != nil {
				return err
			}
		} else {
			err := regularAssign(be, match)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func regularAssign(be pb.BackendServiceClient, match *pb.Match) error {
	ticketIDs := []string{}
	for _, t := range match.GetTickets() {
		ticketIDs = append(ticketIDs, t.Id)
	}

	// Request Connection to AllocateService.
	aloReq, err := http.NewRequest("GET", allocateHostName, nil)
	if err != nil {
		return err
	}
	aloReq.SetBasicAuth(allocateKey, allocatePass)

	client := new(http.Client)
	resp, err := client.Do(aloReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	byteArray, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	var alo AllocateResponce
	json.Unmarshal(byteArray, &alo)
	var conn string
	conn = fmt.Sprintf("%s:%d", alo.Status.Address, alo.Status.Ports[0].Port)

	// GameServerに接続情報を通知しておく
	go noticeConnection(conn)

	req := &pb.AssignTicketsRequest{
		TicketIds: ticketIDs,
		Assignment: &pb.Assignment{
			Connection: conn,
		},
	}

	if _, err := be.AssignTickets(context.Background(), req); err != nil {
		return fmt.Errorf("AssignTickets failed for match %v, got %w", match.GetMatchId(), err)
	}

	log.Printf("Assigned server %v to match %v", conn, match.GetMatchId())
	return nil
}

func noticeConnection(connection string) {
	conn, _ := net.Dial("udp", connection)
	defer conn.Close()
	conn.Write([]byte("CONNECTION " + connection))
	buffer := make([]byte, 1500)
	conn.Read(buffer)
}

func backfillAssign(be pb.BackendServiceClient, match *pb.Match, backfillTicket *pb.Ticket) error {
	// Assigne対象となるBackfillTicketを除外したTicketIDのリストを作成
	ticketIDs := []string{}
	for _, t := range match.GetTickets() {
		if t != backfillTicket {
			ticketIDs = append(ticketIDs, t.Id)
		}
	}

	// BackfillTicketからConnectionを取得し他のTicketに反映
	conn := backfillTicket.GetAssignment().GetConnection()
	req := &pb.AssignTicketsRequest{
		TicketIds: ticketIDs,
		Assignment: &pb.Assignment{
			Connection: conn,
		},
	}
	if _, err := be.AssignTickets(context.Background(), req); err != nil {
		return fmt.Errorf("AssignTickets failed for match %v, got %w", match.GetMatchId(), err)
	}

	log.Printf("Assigned Backfill %v to match %v", conn, match.GetMatchId())

	// BackfillTicketを更新
	extensions := backfillTicket.GetAssignment().GetExtensions()
	joinablePlayerNumByte := extensions["joinablePlayerNum"].GetValue()
	joinablePlayerNumStr := string(joinablePlayerNumByte)
	joinablePlayerNum, err := strconv.Atoi(joinablePlayerNumStr)
	if err != nil {
		return err
	}
	// 参加可能人数を更新
	joinablePlayerNum = joinablePlayerNum - len(ticketIDs)

	var anyJoinablePlayerNum any.Any
	anyJoinablePlayerNum.Value = []byte(strconv.Itoa(joinablePlayerNum))
	extensions["joinablePlayerNum"] = &anyJoinablePlayerNum

	backfillTicketID := []string{}
	backfillTicketID = append(backfillTicketID, backfillTicket.GetId())

	req = &pb.AssignTicketsRequest{
		TicketIds: backfillTicketID,
		Assignment: &pb.Assignment{
			Connection: backfillTicket.GetAssignment().GetConnection(),
			Extensions: extensions,
		},
	}
	if _, err := be.AssignTickets(context.Background(), req); err != nil {
		return fmt.Errorf("Update BackfillTickets failed for match %v, got %w", match.GetMatchId(), err)
	}

	return nil
}
