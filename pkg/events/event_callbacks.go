package events

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/events"
	lexutil "github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/repo"
	"github.com/bluesky-social/indigo/repomgr"
	"github.com/ericvolp12/bsky-experiments/pkg/graph"
	"github.com/ericvolp12/bsky-experiments/pkg/search"
	intXRPC "github.com/ericvolp12/bsky-experiments/pkg/xrpc"
	lru "github.com/hashicorp/golang-lru"
)

// RepoRecord holds data needed for processing a RepoRecord
type RepoRecord struct {
	ctx       context.Context
	pst       appbsky.FeedPost
	opPath    string
	repoName  string
	eventTime string
	workerID  int
}

// BSky is a struct that holds the state of the social graph and the
// authenticated XRPC client
type BSky struct {
	IncludeLinks bool

	SocialGraph    graph.Graph
	SocialGraphMux sync.RWMutex

	// Generate a Profile Cache with a TTL
	profileCache    *lru.ARCCache
	profileCacheTTL time.Duration

	// Generate a Post Cache with a TTL
	postCache    *lru.ARCCache
	postCacheTTL time.Duration

	RepoRecordQueue chan RepoRecord

	WorkerCount int
	Workers     []*Worker

	PostRegistryEnabled bool
	PostRegistry        *search.PostRegistry
}

// NewBSky creates a new BSky struct with an authenticated XRPC client
// and a social graph, initializing mutexes for cross-routine access
func NewBSky(
	ctx context.Context,
	includeLinks, postRegistryEnabled bool,
	dbConnectionString string,
	workerCount int,
) (*BSky, error) {
	postCache, err := lru.NewARC(5000)
	if err != nil {
		return nil, err
	}

	profileCache, err := lru.NewARC(15000)
	if err != nil {
		return nil, err
	}

	var postRegistry *search.PostRegistry

	if postRegistryEnabled {
		postRegistry, err = search.NewPostRegistry(dbConnectionString)
		if err != nil {
			return nil, err
		}
	}

	bsky := &BSky{
		IncludeLinks: includeLinks,

		SocialGraph:    graph.NewGraph(),
		SocialGraphMux: sync.RWMutex{},

		profileCache: profileCache,
		postCache:    postCache,

		// 60 minute Cache TTLs
		profileCacheTTL: time.Minute * 60,
		postCacheTTL:    time.Minute * 60,

		RepoRecordQueue: make(chan RepoRecord, 100),
		WorkerCount:     workerCount,
		Workers:         make([]*Worker, workerCount),

		PostRegistryEnabled: postRegistryEnabled,
		PostRegistry:        postRegistry,
	}

	// Initialize the workers, each with their own BSky Client and Mutex
	// Workers share a single WorkQueue and SocialGraph/Mutex
	for i := 0; i < workerCount; i++ {
		client, err := intXRPC.GetXRPCClient(ctx)
		if err != nil {
			return nil, err
		}

		bsky.Workers[i] = &Worker{
			WorkerID:  i,
			Client:    client,
			ClientMux: sync.RWMutex{},
		}

		go bsky.worker(i)
	}

	return bsky, nil
}

// HandleRepoCommit is called when a repo commit is received and prints its contents
func (bsky *BSky) HandleRepoCommit(evt *comatproto.SyncSubscribeRepos_Commit) error {
	ctx := context.Background()
	rr, err := repo.ReadRepoFromCar(ctx, bytes.NewReader(evt.Blocks))
	if err != nil {
		fmt.Println(err)
	} else {
		for _, op := range evt.Ops {
			ek := repomgr.EventKind(op.Action)
			switch ek {
			case repomgr.EvtKindCreateRecord, repomgr.EvtKindUpdateRecord:
				// Grab the record from the merkel tree
				rc, rec, err := rr.GetRecord(ctx, op.Path)
				if err != nil {
					e := fmt.Errorf("getting record %s (%s) within seq %d for %s: %w", op.Path, *op.Cid, evt.Seq, evt.Repo, err)
					log.Printf("failed to get a record from the event: %+v\n", e)
					return nil
				}

				// Verify that the record cid matches the cid in the event
				if lexutil.LexLink(rc) != *op.Cid {
					e := fmt.Errorf("mismatch in record and op cid: %s != %s", rc, *op.Cid)
					log.Printf("failed to LexLink the record in the event: %+v\n", e)
					return nil
				}

				recordAsCAR := lexutil.LexiconTypeDecoder{
					Val: rec,
				}

				// Attempt to Unpack the CAR Blocks into JSON Byte Array
				b, err := recordAsCAR.MarshalJSON()
				if err != nil {
					log.Printf("failed to marshal record as CAR: %+v\n", err)
					return nil
				}

				// Unmarshal the JSON Byte Array into a FeedPost
				var pst = appbsky.FeedPost{}
				err = json.Unmarshal(b, &pst)
				if err != nil {
					log.Printf("failed to unmarshal post into a FeedPost: %+v\n", err)
					return nil
				}

				// Add the RepoRecord to the Queue
				bsky.RepoRecordQueue <- RepoRecord{
					ctx:       ctx,
					pst:       pst,
					repoName:  evt.Repo,
					opPath:    op.Path,
					eventTime: evt.Time,
				}
			}
		}
	}
	return nil
}

func HandleRepoInfo(info *comatproto.SyncSubscribeRepos_Info) error {
	return nil
}

func HandleError(errf *events.ErrorFrame) error {
	return nil
}
