package main

// The Frontend in this tutorial continously creates Tickets in batches in Open Match.

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo"

	"google.golang.org/grpc"
	"open-match.dev/open-match/pkg/pb"
)

type matchResponce struct {
	IP   string `json:"ip"`
	Port string `json:"port"`
}

const (
	// The endpoint for the Open Match Frontend service.
	omFrontendEndpoint = "om-frontend.open-match.svc.cluster.local:50504"
	// Number of tickets created per iteration
	ticketsPerIter = 20
)

var fe pb.FrontendServiceClient

func main() {
	// Connect to Open Match Frontend.
	conn, err := grpc.Dial(omFrontendEndpoint, grpc.WithInsecure())
	if err != nil {
		log.Fatalf("Failed to connect to Open Match, got %v", err)
	}

	defer conn.Close()
	fe = pb.NewFrontendServiceClient(conn)

	// create REST
	e := echo.New()
	e.GET("/match/:gamemode", handleGetMatch)
	e.POST("/backend/:gamemode", handleRegisterBackfill)
	e.Start(":80")
}

func handleGetMatch(c echo.Context) error {
	matchRes := new(matchResponce)
	if err := c.Bind(matchRes); err != nil {
		log.Printf("Failed to echo Bind, got %v", err)
		return c.JSON(http.StatusInternalServerError, matchRes)
	}

	// Create Ticket.
	gamemode := c.Param("gamemode")
	req := &pb.CreateTicketRequest{
		Ticket: makeTicket(gamemode),
	}
	resp, err := fe.CreateTicket(context.Background(), req)
	if err != nil {
		log.Printf("Failed to CreateTicket, got %v", err)
		return c.JSON(http.StatusInternalServerError, matchRes)
	}
	t := resp.Ticket
	log.Printf("Create Ticket: %v", t.GetId())

	// Polling TicketAssignment.
	for {
		got, err := fe.GetTicket(context.Background(), &pb.GetTicketRequest{TicketId: t.GetId()})
		if err != nil {
			log.Printf("Failed to GetTicket, got %v", err)
			return c.JSON(http.StatusInternalServerError, matchRes)
		}

		if got.GetAssignment() != nil {
			log.Printf("Ticket %v got assignment %v", got.GetId(), got.GetAssignment())
			conn := got.GetAssignment().Connection
			slice := strings.Split(conn, ":")
			matchRes.IP = slice[0]
			matchRes.Port = slice[1]
			break
		}
		time.Sleep(time.Second * 1)
	}

	_, err = fe.DeleteTicket(context.Background(), &pb.DeleteTicketRequest{TicketId: t.GetId()})
	if err != nil {
		log.Printf("Failed to Delete Ticket %v, got %s", t.GetId(), err.Error())
	}
	return c.JSON(http.StatusOK, matchRes)
}

// backfillRequest
type backfillRequest struct {
	Connection        string `json:"connection" form:"connection" query:"connection"`
	JoinablePlayerNum string `json:"joinableplayernum" form:"joinableplayernum" query:"joinableplayernum"`
}

func handleRegisterBackfill(c echo.Context) error {
	backfill := new(backfillRequest)
	if err := c.Bind(backfill); err != nil {
		errstr := fmt.Sprint("Failed to echo Bind, got %v", err)
		log.Printf(errstr)
		return c.String(http.StatusInternalServerError, errstr)
	}

	// Create Ticket.
	gamemode := c.Param("gamemode")
	req := &pb.CreateTicketRequest{
		Ticket: makeBackfillTicket(gamemode, backfill.Connection, backfill.JoinablePlayerNum),
	}
	resp, err := fe.CreateTicket(context.Background(), req)
	if err != nil {
		errstr := fmt.Sprint("Failed to CreateTicket, got %v", err)
		log.Printf(errstr)
		return c.String(http.StatusInternalServerError, errstr)
	}
	t := resp.Ticket
	log.Printf("Create BackfillTicket: %v", t.GetId())

	// Polling TicketAssignment.
	for {
		got, err := fe.GetTicket(context.Background(), &pb.GetTicketRequest{TicketId: t.GetId()})
		if err != nil {
			errstr := fmt.Sprint("Failed to GetTicket, got %v", err)
			log.Printf(errstr)
			return c.String(http.StatusInternalServerError, errstr)
		}

		if got.GetAssignment() != nil {
			extensions := got.GetAssignment().GetExtensions()
			joinablePlayerNumByte := extensions["joinablePlayerNum"].GetValue()
			joinablePlayerNumStr := string(joinablePlayerNumByte)
			joinablePlayerNum, err := strconv.Atoi(joinablePlayerNumStr)
			if err != nil {
				errstr := fmt.Sprint("Failed to Atoi, %v", err)
				log.Printf(errstr)
				return c.String(http.StatusInternalServerError, errstr)
			}

			if joinablePlayerNum <= 0 {
				log.Printf("End Backfill. Ticket(%v) conn(%v)", got.GetId(), got.GetAssignment().Connection)
				break
			}
		}
		time.Sleep(time.Second * 1)
	}

	_, err = fe.DeleteTicket(context.Background(), &pb.DeleteTicketRequest{TicketId: t.GetId()})
	if err != nil {
		log.Printf("Failed to Delete Ticket %v, got %s", t.GetId(), err.Error())
	}
	return c.String(http.StatusOK, "OK")
}
