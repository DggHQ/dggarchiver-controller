package notifications

import (
	"bytes"
	"strings"
	"text/template"

	dggarchivermodel "github.com/DggHQ/dggarchiver-model"
)

var (
	receive = strings.Join([]string{
		"Platform: {{ .Platform }}",
		"ID: {{ .VID }}",
		"Title: {{ .Title }}",
		"Start Time: {{ .StartTime }}",
	}, "\n")

	container = strings.Join([]string{
		"Container Name: {{ .ContainerName }}",
		"Platform: {{ .Platform }}",
		"ID: {{ .VID }}",
		"Title: {{ .Title }}",
		"Start Time: {{ .StartTime }}",
	}, "\n")
)

var (
	receiveTemplate, _   = template.New("receive").Parse(receive)
	containerTemplate, _ = template.New("container").Parse(container)
)

func GetReceiveMessage(vod *dggarchivermodel.VOD) string {
	var b bytes.Buffer

	_ = receiveTemplate.Execute(&b, vod)

	return b.String()
}

type c struct {
	*dggarchivermodel.VOD
	ContainerName string
}

func GetContainerMessage(vod *dggarchivermodel.VOD, containerName string) string {
	var b bytes.Buffer

	_ = containerTemplate.Execute(&b, c{
		VOD:           vod,
		ContainerName: containerName,
	})

	return b.String()
}
