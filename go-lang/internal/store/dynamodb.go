package store

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/t2-f/zabbix-cloning/internal/config"
	"github.com/t2-f/zabbix-cloning/internal/model"
)

type DynamoDB struct {
	client      *dynamodb.Client
	limit, wait int
}

func NewDynamoDB(ctx context.Context, cfg *config.Config) (*DynamoDB, error) {
	options := []func(*awsconfig.LoadOptions) error{awsconfig.WithRegion(cfg.AWSRegion)}
	if cfg.StoreAccess != "" || cfg.StoreCredential != "" {
		options = append(options, awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(cfg.StoreAccess, cfg.StoreCredential, "")))
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, options...)
	if err != nil {
		return nil, err
	}
	client := dynamodb.NewFromConfig(awsCfg, func(options *dynamodb.Options) {
		if cfg.AWSEndpointURL != "" {
			options.BaseEndpoint = aws.String(cfg.AWSEndpointURL)
		}
	})
	store := &DynamoDB{client: client, limit: cfg.StoreLimit, wait: cfg.StoreInterval}
	if _, err := client.DescribeTable(ctx, &dynamodb.DescribeTableInput{TableName: aws.String("ZC_VERSION")}); err != nil {
		return nil, err
	}
	if _, err := client.DescribeTable(ctx, &dynamodb.DescribeTableInput{TableName: aws.String("ZC_DATA")}); err != nil {
		return nil, err
	}
	return store, nil
}

func (d *DynamoDB) Versions(ctx context.Context, versionID string) ([]model.Version, error) {
	input := &dynamodb.ScanInput{TableName: aws.String("ZC_VERSION")}
	var versions []model.Version
	for {
		result, err := d.client.Scan(ctx, input)
		if err != nil {
			return nil, err
		}
		var batch []model.Version
		for _, item := range result.Items {
			var value map[string]any
			if err := attributevalue.UnmarshalMap(item, &value); err != nil {
				return nil, err
			}
			batch = append(batch, model.Version{
				VersionID: model.String(value["VERSION_ID"]), UnixTime: int64(model.Float(value["UNIXTIME"])),
				MasterVersion: model.Float(value["MASTER_VERSION"]), Description: model.String(value["DESCRIPTION"]),
			})
		}
		for _, version := range batch {
			if versionID == "" || version.VersionID == versionID {
				versions = append(versions, version)
			}
		}
		if len(result.LastEvaluatedKey) == 0 {
			break
		}
		input.ExclusiveStartKey = result.LastEvaluatedKey
	}
	sort.Slice(versions, func(i, j int) bool { return versions[i].UnixTime > versions[j].UnixTime })
	return versions, nil
}

func (d *DynamoDB) Load(ctx context.Context, version model.Version) (model.Dataset, error) {
	values, err := attributevalue.MarshalMap(map[string]any{":version": version.VersionID})
	if err != nil {
		return nil, err
	}
	input := &dynamodb.QueryInput{TableName: aws.String("ZC_DATA"), KeyConditionExpression: aws.String("VERSION_ID = :version"), ExpressionAttributeValues: values}
	var records []model.Record
	for {
		result, err := d.client.Query(ctx, input)
		if err != nil {
			return nil, err
		}
		var batch []model.Record
		if err := attributevalue.UnmarshalListOfMaps(result.Items, &batch); err != nil {
			return nil, err
		}
		for i := range batch {
			if err := decompressJSON(batch[i].Payload, &batch[i].Data); err != nil {
				return nil, fmt.Errorf("record %s: %w", batch[i].DataID, err)
			}
		}
		records = append(records, batch...)
		if len(result.LastEvaluatedKey) == 0 {
			break
		}
		input.ExclusiveStartKey = result.LastEvaluatedKey
	}
	return model.DatasetFromRecords(records), nil
}

func (d *DynamoDB) Save(ctx context.Context, version model.Version, dataset model.Dataset) error {
	records := dataset.Records(version.VersionID)
	requests := make([]types.WriteRequest, 0, 25)
	for index := range records {
		payload, err := compressJSON(records[index].Data)
		if err != nil {
			return err
		}
		records[index].Payload = payload
		item, err := attributevalue.MarshalMap(records[index])
		if err != nil {
			return err
		}
		requests = append(requests, types.WriteRequest{PutRequest: &types.PutRequest{Item: item}})
		if len(requests) == 25 || index == len(records)-1 {
			if err := d.writeBatch(ctx, "ZC_DATA", requests); err != nil {
				return err
			}
			requests = requests[:0]
			if d.limit > 0 && index > 0 && (index+1)%d.limit == 0 && d.wait > 0 {
				time.Sleep(time.Duration(d.wait) * time.Second)
			}
		}
	}
	versionItem, err := attributevalue.MarshalMap(version)
	if err != nil {
		return err
	}
	_, err = d.client.PutItem(ctx, &dynamodb.PutItemInput{TableName: aws.String("ZC_VERSION"), Item: versionItem})
	return err
}

func (d *DynamoDB) Clear(ctx context.Context, table string) error {
	tables := []string{table}
	if table == "ALL" {
		tables = []string{"VERSION", "DATA"}
	}
	for _, name := range tables {
		if name != "VERSION" && name != "DATA" {
			return fmt.Errorf("invalid table %q", name)
		}
		if err := d.clearTable(ctx, "ZC_"+name); err != nil {
			return err
		}
	}
	return nil
}

func (d *DynamoDB) clearTable(ctx context.Context, table string) error {
	input := &dynamodb.ScanInput{TableName: aws.String(table), ProjectionExpression: aws.String("VERSION_ID, #sort")}
	if table == "ZC_VERSION" {
		input.ExpressionAttributeNames = map[string]string{"#sort": "UNIXTIME"}
	} else {
		input.ExpressionAttributeNames = map[string]string{"#sort": "DATA_ID"}
	}
	for {
		result, err := d.client.Scan(ctx, input)
		if err != nil {
			return err
		}
		requests := make([]types.WriteRequest, 0, 25)
		for _, item := range result.Items {
			requests = append(requests, types.WriteRequest{DeleteRequest: &types.DeleteRequest{Key: item}})
			if len(requests) == 25 {
				if err := d.writeBatch(ctx, table, requests); err != nil {
					return err
				}
				requests = requests[:0]
			}
		}
		if len(requests) > 0 {
			if err := d.writeBatch(ctx, table, requests); err != nil {
				return err
			}
		}
		if len(result.LastEvaluatedKey) == 0 {
			break
		}
		input.ExclusiveStartKey = result.LastEvaluatedKey
	}
	return nil
}

func (d *DynamoDB) writeBatch(ctx context.Context, table string, requests []types.WriteRequest) error {
	remaining := map[string][]types.WriteRequest{table: requests}
	for len(remaining) > 0 {
		result, err := d.client.BatchWriteItem(ctx, &dynamodb.BatchWriteItemInput{RequestItems: remaining})
		if err != nil {
			return err
		}
		remaining = result.UnprocessedItems
		if len(remaining) > 0 {
			time.Sleep(500 * time.Millisecond)
		}
	}
	return nil
}

func (d *DynamoDB) DeleteRecord(ctx context.Context, versionID, dataID string) error {
	key, err := attributevalue.MarshalMap(map[string]any{"VERSION_ID": versionID, "DATA_ID": dataID})
	if err != nil {
		return err
	}
	_, err = d.client.DeleteItem(ctx, &dynamodb.DeleteItemInput{TableName: aws.String("ZC_DATA"), Key: key})
	return err
}
func (d *DynamoDB) DeleteVersion(ctx context.Context, versionID string) error {
	versions, err := d.Versions(ctx, versionID)
	if err != nil {
		return err
	}
	for _, version := range versions {
		key, err := attributevalue.MarshalMap(map[string]any{"VERSION_ID": version.VersionID, "UNIXTIME": version.UnixTime})
		if err != nil {
			return err
		}
		if _, err = d.client.DeleteItem(ctx, &dynamodb.DeleteItemInput{TableName: aws.String("ZC_VERSION"), Key: key}); err != nil {
			return err
		}
	}
	data, err := d.Load(ctx, model.Version{VersionID: versionID})
	if err != nil {
		return err
	}
	for _, record := range data.Records(versionID) {
		if err := d.DeleteRecord(ctx, versionID, record.DataID); err != nil {
			return err
		}
	}
	return nil
}
func (d *DynamoDB) Close() error { return nil }
