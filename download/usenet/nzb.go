package usenet

import (
	"encoding/xml"
	"io"
)

type NZB struct {
	XMLName xml.Name  `xml:"nzb"`
	Files   []NZBFile `xml:"file"`
}

type NZBFile struct {
	Poster   string       `xml:"poster,attr"`
	Date     int64        `xml:"date,attr"`
	Subject  string       `xml:"subject,attr"`
	Groups   []string     `xml:"groups>group"`
	Segments []NZBSegment `xml:"segments>segment"`
}

type NZBSegment struct {
	XMLName   xml.Name `xml:"segment"`
	Bytes     int64    `xml:"bytes,attr"`
	Number    int      `xml:"number,attr"`
	MessageID string   `xml:",chardata"`
}

func ParseNZB(r io.Reader) (*NZB, error) {
	var nzb NZB
	if err := xml.NewDecoder(r).Decode(&nzb); err != nil {
		return nil, err
	}
	return &nzb, nil
}
