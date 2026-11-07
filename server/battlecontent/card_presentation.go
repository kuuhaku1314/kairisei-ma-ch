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
	Modern     bool
}

func cardPresentationColumns(width int) (picture, limit int) {
	if width >= 113 {
		return 50, 34
	}
	return 36, 22
}

func ParseCardPresentation(row []string) (CardPresentation, error) {
	card, err := ParseCard(row)
	if err != nil {
		return CardPresentation{}, err
	}
	pictureColumn, limitColumn := cardPresentationColumns(len(row))
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
	picture, err := integer(pictureColumn, "PictID")
	if err != nil {
		return CardPresentation{}, err
	}
	limit, err := integer(limitColumn, "limit count")
	if err != nil {
		return CardPresentation{}, err
	}
	return CardPresentation{ID: card.ID, Prefix: Field(row, 4), Name: strings.TrimSpace(Field(row, 5)),
		PictureID: picture, LimitCount: limit, Modern: len(row) >= 113}, nil
}

// Headerless JP data starts with a real numeric row. A legacy named header,
// when present, still has to declare the picture column in the expected slot.
func ValidateCardPresentationHeader(row []string) error {
	if len(row) == 0 || strings.TrimPrefix(row[0], "\ufeff") != "#ID" {
		return nil
	}
	picture, _ := cardPresentationColumns(len(row))
	if Field(row, picture) != "PictID" {
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
