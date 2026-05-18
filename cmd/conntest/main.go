// Command conntest creates a temporary Google Slides presentation to verify
// connection site indices for each shape type (Rectangle, Round_Rectangle,
// Ellipse, Diamond). It creates shapes with connectors at each site index
// and prints the results.
//
// Usage:
//
//	go run cmd/conntest/main.go [--credentials <creds.json>] [--keep]
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/owulveryck/agentigslide/internal/auth"
	"github.com/owulveryck/agentigslide/internal/config"

	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
	"google.golang.org/api/slides/v1"
)

type shapeTest struct {
	name      string
	shapeType string
}

func main() {
	credentials := flag.String("credentials", "", "Path to OAuth2 client credentials JSON (optional; uses ADC if omitted)")
	keep := flag.Bool("keep", false, "Keep the test presentation (don't delete it)")
	flag.Parse()

	slidesCfg, err := config.LoadSlidesConfig()
	if err != nil {
		log.Fatalf("Configuration error: %v", err)
	}

	credFile := *credentials
	if credFile == "" {
		credFile = slidesCfg.Credentials
	}

	ctx := context.Background()
	oauthClient, err := auth.GetOAuthClient(ctx, credFile)
	if err != nil {
		log.Fatalf("Failed to get authenticated client: %v", err)
	}

	slidesSrv, err := slides.NewService(ctx, option.WithHTTPClient(oauthClient))
	if err != nil {
		log.Fatalf("Failed to create Slides service: %v", err)
	}

	driveSrv, err := drive.NewService(ctx, option.WithHTTPClient(oauthClient))
	if err != nil {
		log.Fatalf("Failed to create Drive service: %v", err)
	}

	presID, err := runTest(ctx, slidesSrv, driveSrv)
	if err != nil {
		log.Fatalf("Test failed: %v", err)
	}

	if *keep {
		fmt.Printf("\nPresentation kept: https://docs.google.com/presentation/d/%s/edit\n", presID)
	} else {
		fmt.Println("\nDeleting test presentation...")
		if err := driveSrv.Files.Delete(presID).Do(); err != nil {
			log.Printf("Warning: failed to delete presentation: %v", err)
		} else {
			fmt.Println("Deleted.")
		}
	}
}

func runTest(ctx context.Context, slidesSrv *slides.Service, driveSrv *drive.Service) (string, error) {
	pres, err := slidesSrv.Presentations.Create(&slides.Presentation{
		Title: "ConnTest - Connection Site Verification",
	}).Do()
	if err != nil {
		return "", fmt.Errorf("create presentation: %w", err)
	}
	presID := pres.PresentationId
	fmt.Printf("Created presentation: %s\n", presID)

	shapes := []shapeTest{
		{"Rectangle", "RECTANGLE"},
		{"RoundRect", "ROUND_RECTANGLE"},
		{"Ellipse", "ELLIPSE"},
		{"Diamond", "DIAMOND"},
	}

	pageID := "conntest_page"
	var reqs []*slides.Request
	reqs = append(reqs, &slides.Request{
		CreateSlide: &slides.CreateSlideRequest{ObjectId: pageID},
	})

	nodeW := int64(1200000)
	nodeH := int64(800000)
	startX := int64(500000)
	spacing := int64(2200000)

	for si, shape := range shapes {
		shapeObjID := fmt.Sprintf("shape_%d", si)
		x := startX + int64(si)*spacing
		y := int64(2500000)

		reqs = append(reqs, &slides.Request{
			CreateShape: &slides.CreateShapeRequest{
				ObjectId:  shapeObjID,
				ShapeType: shape.shapeType,
				ElementProperties: &slides.PageElementProperties{
					PageObjectId: pageID,
					Size: &slides.Size{
						Width:  &slides.Dimension{Magnitude: float64(nodeW), Unit: "EMU"},
						Height: &slides.Dimension{Magnitude: float64(nodeH), Unit: "EMU"},
					},
					Transform: &slides.AffineTransform{
						ScaleX: 1, ScaleY: 1,
						TranslateX: float64(x),
						TranslateY: float64(y),
						Unit:       "EMU",
					},
				},
			},
		})

		reqs = append(reqs, &slides.Request{
			InsertText: &slides.InsertTextRequest{
				ObjectId: shapeObjID,
				Text:     shape.name,
			},
		})

		for site := int64(0); site < 4; site++ {
			connID := fmt.Sprintf("conn_%d_%d", si, site)
			dotID := fmt.Sprintf("dot_%d_%d", si, site)

			var dotX, dotY float64
			switch site {
			case 0: // expected: top
				dotX = float64(x) + float64(nodeW)/2 - 50000
				dotY = float64(y) - 600000
			case 1: // expected: right
				dotX = float64(x) + float64(nodeW) + 400000
				dotY = float64(y) + float64(nodeH)/2 - 50000
			case 2: // expected: bottom
				dotX = float64(x) + float64(nodeW)/2 - 50000
				dotY = float64(y) + float64(nodeH) + 400000
			case 3: // expected: left
				dotX = float64(x) - 600000
				dotY = float64(y) + float64(nodeH)/2 - 50000
			}

			reqs = append(reqs, &slides.Request{
				CreateShape: &slides.CreateShapeRequest{
					ObjectId:  dotID,
					ShapeType: "ELLIPSE",
					ElementProperties: &slides.PageElementProperties{
						PageObjectId: pageID,
						Size: &slides.Size{
							Width:  &slides.Dimension{Magnitude: 100000, Unit: "EMU"},
							Height: &slides.Dimension{Magnitude: 100000, Unit: "EMU"},
						},
						Transform: &slides.AffineTransform{
							ScaleX: 1, ScaleY: 1,
							TranslateX: dotX,
							TranslateY: dotY,
							Unit:       "EMU",
						},
					},
				},
			})

			reqs = append(reqs, &slides.Request{
				InsertText: &slides.InsertTextRequest{
					ObjectId: dotID,
					Text:     fmt.Sprintf("%d", site),
				},
			})

			reqs = append(reqs, &slides.Request{
				CreateLine: &slides.CreateLineRequest{
					ObjectId: connID,
					Category: "STRAIGHT",
					ElementProperties: &slides.PageElementProperties{
						PageObjectId: pageID,
						Size: &slides.Size{
							Width:  &slides.Dimension{Magnitude: 100000, Unit: "EMU"},
							Height: &slides.Dimension{Magnitude: 100000, Unit: "EMU"},
						},
						Transform: &slides.AffineTransform{
							ScaleX: 1, ScaleY: 1,
							TranslateX: float64(x),
							TranslateY: float64(y),
							Unit:       "EMU",
						},
					},
				},
			})

			reqs = append(reqs, &slides.Request{
				UpdateLineProperties: &slides.UpdateLinePropertiesRequest{
					ObjectId: connID,
					LineProperties: &slides.LineProperties{
						StartConnection: &slides.LineConnection{
							ConnectedObjectId:   shapeObjID,
							ConnectionSiteIndex: site,
							ForceSendFields:     []string{"ConnectionSiteIndex"},
						},
						EndConnection: &slides.LineConnection{
							ConnectedObjectId:   dotID,
							ConnectionSiteIndex: 0,
							ForceSendFields:     []string{"ConnectionSiteIndex"},
						},
						EndArrow: "OPEN_ARROW",
					},
					Fields: "startConnection,endConnection,endArrow",
				},
			})
		}
	}

	if _, err := slidesSrv.Presentations.BatchUpdate(presID, &slides.BatchUpdatePresentationRequest{
		Requests: reqs,
	}).Do(); err != nil {
		return presID, fmt.Errorf("batch update: %w", err)
	}

	// Delete the default first slide
	if len(pres.Slides) > 0 {
		defaultPageID := pres.Slides[0].ObjectId
		_, err := slidesSrv.Presentations.BatchUpdate(presID, &slides.BatchUpdatePresentationRequest{
			Requests: []*slides.Request{{
				DeleteObject: &slides.DeleteObjectRequest{ObjectId: defaultPageID},
			}},
		}).Do()
		if err != nil {
			log.Printf("Warning: could not delete default slide: %v", err)
		}
	}

	// Now re-read the presentation and inspect connectors
	pres, err = slidesSrv.Presentations.Get(presID).Do()
	if err != nil {
		return presID, fmt.Errorf("re-read presentation: %w", err)
	}

	fmt.Print("\n=== Connection Site Verification Results ===\n\n")

	for _, page := range pres.Slides {
		for _, el := range page.PageElements {
			if el.Line == nil {
				continue
			}
			lp := el.Line.LineProperties
			if lp == nil || lp.StartConnection == nil {
				continue
			}
			fmt.Printf("Connector %s:\n", el.ObjectId)
			fmt.Printf("  Start: shape=%s site=%d\n",
				lp.StartConnection.ConnectedObjectId,
				lp.StartConnection.ConnectionSiteIndex)
			if lp.EndConnection != nil {
				fmt.Printf("  End:   shape=%s site=%d\n",
					lp.EndConnection.ConnectedObjectId,
					lp.EndConnection.ConnectionSiteIndex)
			}

			// Show the connector's actual position to verify which side it connects from
			if el.Transform != nil {
				fmt.Printf("  Position: translateX=%.0f translateY=%.0f\n",
					el.Transform.TranslateX, el.Transform.TranslateY)
			}
			fmt.Println()
		}
	}

	fmt.Println("Visual inspection: open the presentation and verify each numbered dot (0-3)")
	fmt.Println("connects to the expected side: 0=top, 1=right, 2=bottom, 3=left")
	fmt.Printf("\nhttps://docs.google.com/presentation/d/%s/edit\n", presID)

	return presID, nil
}

func init() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: conntest [--credentials <creds.json>] [--keep]\n\n")
		fmt.Fprintf(os.Stderr, "Creates a test presentation to verify connection site indices.\n\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nEnvironment:\n  SLIDES_TEMPLATE_ID   required (any valid ID)\n  SLIDES_CREDENTIALS   path to credentials JSON\n")
	}
}
