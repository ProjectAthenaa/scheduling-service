package scheduler

import (
	"github.com/ProjectAthenaa/scheduling-service/helpers"
	"github.com/ProjectAthenaa/sonic-core/sonic/database/ent"
	"github.com/ProjectAthenaa/sonic-core/sonic/database/ent/product"
	"sort"
	"strings"
)

type Tasks []*Task

type Task struct {
	*ent.Task
	monitorChannel    string
	subscriptionToken string
	controlToken      string
}

func (t Tasks) Chunk(chunkSize int) []Tasks {
	if len(t) == 0 {
		return nil
	}
	divided := make([]Tasks, (len(t)+chunkSize-1)/chunkSize)
	prev := 0
	i := 0
	till := len(t) - chunkSize
	for prev < till {
		next := prev + chunkSize
		divided[i] = t[prev:next]
		prev = next
		i++
	}
	divided[i] = t[prev:]
	return divided
}

func (t *Task) getMonitorID() string {
	v := t.Edges.Product[0]
	switch v.LookupType {
	case product.LookupTypeLink:
		return helpers.SHA1(v.Link)
	case product.LookupTypeKeywords:
		sort.Strings(v.PositiveKeywords)
		sort.Strings(v.NegativeKeywords)

		for i, s := range v.PositiveKeywords {
			v.PositiveKeywords[i] = strings.ToLower(s)
		}
		for i, s := range v.NegativeKeywords {
			v.NegativeKeywords[i] = strings.ToLower(s)
		}

		return helpers.SHA1(strings.Join(v.PositiveKeywords, "") + strings.Join(v.NegativeKeywords, ""))
	case product.LookupTypeOther:
		for k, val := range v.Metadata {
			if strings.Contains(k, "LOOKUP") {
				return helpers.SHA1(val)
			}
		}
	}
	return ""
}
