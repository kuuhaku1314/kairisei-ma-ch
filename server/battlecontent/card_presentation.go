package battlecontent

import (
	"fmt"
	"io"
	"strconv"
	"strings"
)

// CardPresentation is content metadata shared by the operations catalog and
// resource lookup. It never renumbers a card or chooses a replacement picture.
type CardPresentation struct {
	ID         int
	Prefix     string
	Name       string
	PictureID  int
	LimitCount int
}

func ParseCardPresentation(row []string) (CardPresentation, error) {
	card, err := ParseCard(row)
	if err != nil {
		return CardPresentation{}, err
	}
	integer := func(column int, name string) (int, error) {
		text := strings.TrimSpace(Field(row, column))
		if text == "" {
			return 0, nil
		}
		value, err := strconv.Atoi(text)
		if err != nil || value < 0 {
			return 0, fmt.Errorf("card %d has invalid %s", card.ID, name)
		}
		return value, nil
	}
	picture, err := integer(50, "PictID")
	if err != nil {
		return CardPresentation{}, err
	}
	limit, err := integer(34, "limit count")
	if err != nil {
		return CardPresentation{}, err
	}
	return CardPresentation{ID: card.ID, Prefix: Field(row, 4), Name: strings.TrimSpace(Field(row, 5)),
		PictureID: picture, LimitCount: limit}, nil
}

// A named header, when present, declares the picture column in the same slot
// as headerless data rows.
func ValidateCardPresentationHeader(row []string) error {
	if len(row) == 0 || strings.TrimPrefix(row[0], "\ufeff") != "#ID" {
		return nil
	}
	if Field(row, 50) != "PictID" {
		return fmt.Errorf("card PictID header differs")
	}
	return nil
}

func ReadCardCSV(path string, consume func([]string) error) error {
	return readCombatCardCSV(path, consume)
}

func ReadCardData(stream io.Reader, consume func([]string) error) error {
	return readCombatCardData(stream, consume)
}
