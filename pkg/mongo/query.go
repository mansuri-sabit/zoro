package mongo

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// QueryBuilder provides a fluent interface for MongoDB queries
type QueryBuilder struct {
	collection *mongo.Collection
	filter     bson.M
	sort       bson.D
	limit      *int64
	skip       *int64
	projection bson.M
}

// NewQuery creates a new query builder for a collection
func (c *Client) NewQuery(collectionName string) *QueryBuilder {
	return &QueryBuilder{
		collection: c.Collection(collectionName),
		filter:     bson.M{},
		projection: bson.M{},
	}
}

// Eq adds an equality filter
func (q *QueryBuilder) Eq(field string, value interface{}) *QueryBuilder {
	q.filter[field] = value
	return q
}

// In adds an "in" filter
func (q *QueryBuilder) In(field string, values interface{}) *QueryBuilder {
	q.filter[field] = bson.M{"$in": values}
	return q
}

// IsNull adds a null check filter
func (q *QueryBuilder) IsNull(field string) *QueryBuilder {
	q.filter[field] = nil
	return q
}

// IsNotNull adds a not-null check filter
func (q *QueryBuilder) IsNotNull(field string) *QueryBuilder {
	q.filter[field] = bson.M{"$ne": nil}
	return q
}

// Filter adds a custom filter (for complex queries like array contains)
func (q *QueryBuilder) Filter(field, operator string, value interface{}) *QueryBuilder {
	switch operator {
	case "cs": // contains (for arrays) - in MongoDB, array contains is direct match
		// For tags array containing a value, use direct match
		q.filter[field] = value
	case "eq":
		q.filter[field] = value
	default:
		q.filter[field] = value
	}
	return q
}

// Gte adds a greater than or equal filter
func (q *QueryBuilder) Gte(field string, value interface{}) *QueryBuilder {
	if existing, ok := q.filter[field].(bson.M); ok {
		existing["$gte"] = value
		q.filter[field] = existing
	} else {
		q.filter[field] = bson.M{"$gte": value}
	}
	return q
}

// Lte adds a less than or equal filter
func (q *QueryBuilder) Lte(field string, value interface{}) *QueryBuilder {
	if existing, ok := q.filter[field].(bson.M); ok {
		existing["$lte"] = value
		q.filter[field] = existing
	} else {
		q.filter[field] = bson.M{"$lte": value}
	}
	return q
}

// Select sets the projection (fields to return)
func (q *QueryBuilder) Select(fields ...string) *QueryBuilder {
	if len(fields) == 0 {
		q.projection = bson.M{}
		return q
	}
	projection := bson.M{}
	for _, field := range fields {
		if field == "*" {
			projection = bson.M{}
			break
		}
		projection[field] = 1
	}
	q.projection = projection
	return q
}

// Limit sets the limit
func (q *QueryBuilder) Limit(limit int64) *QueryBuilder {
	q.limit = &limit
	return q
}

// Skip sets the skip value
func (q *QueryBuilder) Skip(skip int64) *QueryBuilder {
	q.skip = &skip
	return q
}

// Sort sets the sort order
func (q *QueryBuilder) Sort(field string, ascending bool) *QueryBuilder {
	direction := 1
	if !ascending {
		direction = -1
	}
	q.sort = append(q.sort, bson.E{Key: field, Value: direction})
	return q
}

// Find executes a find query and returns results
func (q *QueryBuilder) Find(ctx context.Context) ([]map[string]interface{}, error) {
	opts := options.Find()
	if q.limit != nil {
		opts.SetLimit(*q.limit)
	}
	if q.skip != nil {
		opts.SetSkip(*q.skip)
	}
	if len(q.sort) > 0 {
		opts.SetSort(q.sort)
	}
	if len(q.projection) > 0 {
		opts.SetProjection(q.projection)
	}

	cursor, err := q.collection.Find(ctx, q.filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var results []map[string]interface{}
	if err := cursor.All(ctx, &results); err != nil {
		return nil, err
	}

	return results, nil
}

// FindOne executes a find one query
func (q *QueryBuilder) FindOne(ctx context.Context) (map[string]interface{}, error) {
	opts := options.FindOne()
	if len(q.projection) > 0 {
		opts.SetProjection(q.projection)
	}
	if len(q.sort) > 0 {
		opts.SetSort(q.sort)
	}

	var result map[string]interface{}
	err := q.collection.FindOne(ctx, q.filter, opts).Decode(&result)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return result, nil
}

// Count returns the count of matching documents
func (q *QueryBuilder) Count(ctx context.Context) (int64, error) {
	return q.collection.CountDocuments(ctx, q.filter)
}

// Insert inserts a document
func (q *QueryBuilder) Insert(ctx context.Context, document interface{}) (interface{}, error) {
	result, err := q.collection.InsertOne(ctx, document)
	if err != nil {
		return nil, err
	}
	return result.InsertedID, nil
}

// InsertMany inserts multiple documents
func (q *QueryBuilder) InsertMany(ctx context.Context, documents []interface{}) ([]interface{}, error) {
	result, err := q.collection.InsertMany(ctx, documents)
	if err != nil {
		return nil, err
	}
	return result.InsertedIDs, nil
}

// Upsert updates or inserts a document
func (q *QueryBuilder) Upsert(ctx context.Context, filter bson.M, update interface{}) (*mongo.UpdateResult, error) {
	opts := options.Update().SetUpsert(true)
	result, err := q.collection.UpdateOne(ctx, filter, bson.M{"$set": update}, opts)
	if err != nil {
		return nil, err
	}
	return result, nil
}

// Update updates matching documents
func (q *QueryBuilder) Update(ctx context.Context, update interface{}) (*mongo.UpdateResult, error) {
	result, err := q.collection.UpdateMany(ctx, q.filter, bson.M{"$set": update})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// UpdateOne updates a single matching document
func (q *QueryBuilder) UpdateOne(ctx context.Context, update interface{}) (*mongo.UpdateResult, error) {
	result, err := q.collection.UpdateOne(ctx, q.filter, bson.M{"$set": update})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// Delete deletes matching documents
func (q *QueryBuilder) Delete(ctx context.Context) (*mongo.DeleteResult, error) {
	result, err := q.collection.DeleteMany(ctx, q.filter)
	if err != nil {
		return nil, err
	}
	return result, nil
}

// DeleteOne deletes a single matching document
func (q *QueryBuilder) DeleteOne(ctx context.Context) (*mongo.DeleteResult, error) {
	result, err := q.collection.DeleteOne(ctx, q.filter)
	if err != nil {
		return nil, err
	}
	return result, nil
}

// Helper function to convert string ID to ObjectID
func StringToObjectID(id string) (primitive.ObjectID, error) {
	return primitive.ObjectIDFromHex(id)
}

// Helper function to convert ObjectID to string
func ObjectIDToString(id primitive.ObjectID) string {
	return id.Hex()
}

// Helper to convert int64 ID to string (for compatibility)
func Int64ToString(id int64) string {
	return fmt.Sprintf("%d", id)
}

// Helper to convert string to int64 (for compatibility)
func StringToInt64(s string) (int64, error) {
	var id int64
	_, err := fmt.Sscanf(s, "%d", &id)
	return id, err
}

// Helper to add timestamps to document
func AddTimestamps(doc map[string]interface{}) {
	now := time.Now().Format(time.RFC3339)
	doc["created_at"] = now
	doc["updated_at"] = now
}

// Helper to update timestamp
func UpdateTimestamp(doc map[string]interface{}) {
	doc["updated_at"] = time.Now().Format(time.RFC3339)
}

