package Entities

type ArcStatus int

const (
	PendingArc  ArcStatus = 0
	ActiveArc   ArcStatus = 1
	ArchivedArc ArcStatus = 2
	FinishedArc ArcStatus = 3
)
