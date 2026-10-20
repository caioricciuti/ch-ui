package database

import "testing"

func TestCreatePipelineWithGraph(t *testing.T) {
	db := openTestDB(t)
	conn, err := db.CreateConnection(CreateConnectionParams{Name: "c", TunnelToken: "tok"})
	if err != nil {
		t.Fatal(err)
	}
	graph := func(id string) ([]PipelineNode, []PipelineEdge) {
		return []PipelineNode{
				{ID: id + "-a", NodeType: "source_ingest", ConfigEncrypted: `{"signal":"logs"}`},
				{ID: id + "-b", NodeType: "sink_clickhouse", ConfigEncrypted: `{}`},
			}, []PipelineEdge{
				{ID: id + "-e", SourceNodeID: id + "-a", TargetNodeID: id + "-b"},
			}
	}
	id, err := db.CreatePipelineWithGraph("p", "d", conn, "alice", graph, `{"zoom":1}`)
	if err != nil {
		t.Fatal(err)
	}
	p, err := db.GetPipelineByID(id)
	if err != nil || p == nil || p.Status != "draft" || p.Config != `{"zoom":1}` || p.CreatedBy == nil || *p.CreatedBy != "alice" || p.Description == nil || *p.Description != "d" {
		t.Fatalf("pipeline: %+v %v", p, err)
	}
	nodes, edges, err := db.GetPipelineGraph(id)
	if err != nil || len(nodes) != 2 || len(edges) != 1 || edges[0].SourceNodeID != id+"-a" {
		t.Fatalf("graph: %v %v %v", nodes, edges, err)
	}

	// A graph that fails to insert leaves no pipeline behind: the edge
	// points at a node that does not exist.
	bad := func(id string) ([]PipelineNode, []PipelineEdge) {
		return nil, []PipelineEdge{{ID: id + "-e", SourceNodeID: "nope", TargetNodeID: "nope"}}
	}
	if _, err := db.CreatePipelineWithGraph("broken", "", conn, "alice", bad, ""); err == nil {
		t.Fatal("a broken graph was accepted")
	}
	all, err := db.GetPipelines()
	if err != nil || len(all) != 1 || all[0].ID != id {
		t.Fatalf("pipelines after the failed create: %+v %v", all, err)
	}
}
