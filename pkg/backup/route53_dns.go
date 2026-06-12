package backup

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/route53"
	"github.com/sirupsen/logrus"
)

// route53DefaultRegion is used when no region is configured. Route53 is a
// global service, but the SDK still requires a region for endpoint resolution.
const route53DefaultRegion = "us-east-1"

// newRoute53Client creates a Route53 client. Static credentials from the
// configuration take precedence; otherwise the default AWS credential chain
// (environment, shared config, instance role) is used.
func (r53 *RealRoute53Manager) newRoute53Client() (*route53.Route53, error) {
	awsCfg := aws.Config{}

	region := r53.config.Region
	if region == "" {
		region = route53DefaultRegion
	}
	awsCfg.Region = aws.String(region)

	if r53.config.AccessKeyID != "" && r53.config.SecretAccessKey != "" {
		awsCfg.Credentials = credentials.NewStaticCredentials(
			r53.config.AccessKeyID,
			r53.config.SecretAccessKey,
			r53.config.SessionToken,
		)
	}

	sess, err := session.NewSessionWithOptions(session.Options{
		SharedConfigState: session.SharedConfigEnable,
		Config:            awsCfg,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create AWS session: %w", err)
	}

	return route53.New(sess), nil
}

// upsertRecord performs a ChangeResourceRecordSets UPSERT for the record and
// optionally waits for the change to reach INSYNC status.
func (r53 *RealRoute53Manager) upsertRecord(ctx context.Context, record DNSRecord) error {
	client, err := r53.newRoute53Client()
	if err != nil {
		return err
	}

	rdata, err := formatRDataValue(record)
	if err != nil {
		return err
	}

	ttl := effectiveTTL(record.TTL, 0)

	input := &route53.ChangeResourceRecordSetsInput{
		HostedZoneId: aws.String(r53.zoneID),
		ChangeBatch: &route53.ChangeBatch{
			Comment: aws.String("siprec-server failover update"),
			Changes: []*route53.Change{
				{
					Action: aws.String(route53.ChangeActionUpsert),
					ResourceRecordSet: &route53.ResourceRecordSet{
						Name: aws.String(dnsFQDN(record.Name)),
						Type: aws.String(record.Type),
						TTL:  aws.Int64(int64(ttl)),
						ResourceRecords: []*route53.ResourceRecord{
							{Value: aws.String(rdata)},
						},
					},
				},
			},
		},
	}

	output, err := client.ChangeResourceRecordSetsWithContext(ctx, input)
	if err != nil {
		return fmt.Errorf("Route53 change request failed: %w", err)
	}

	changeID := aws.StringValue(output.ChangeInfo.Id)
	r53.logger.WithFields(logrus.Fields{
		"change_id": changeID,
		"status":    aws.StringValue(output.ChangeInfo.Status),
		"name":      record.Name,
		"type":      record.Type,
	}).Info("Route53 change submitted")

	if r53.WaitForSync {
		err = client.WaitUntilResourceRecordSetsChangedWithContext(ctx, &route53.GetChangeInput{
			Id: aws.String(changeID),
		})
		if err != nil {
			return fmt.Errorf("Route53 change %s did not reach INSYNC: %w", changeID, err)
		}
		r53.logger.WithField("change_id", changeID).Info("Route53 change is INSYNC")
	}

	return nil
}

// listRecords retrieves resource record sets matching the given name and type
// using ListResourceRecordSets.
func (r53 *RealRoute53Manager) listRecords(ctx context.Context, name, recordType string) ([]DNSRecord, error) {
	client, err := r53.newRoute53Client()
	if err != nil {
		return nil, err
	}

	fqdn := dnsFQDN(name)

	output, err := client.ListResourceRecordSetsWithContext(ctx, &route53.ListResourceRecordSetsInput{
		HostedZoneId:    aws.String(r53.zoneID),
		StartRecordName: aws.String(fqdn),
		StartRecordType: aws.String(recordType),
		MaxItems:        aws.String("100"),
	})
	if err != nil {
		return nil, fmt.Errorf("Route53 list request failed: %w", err)
	}

	var records []DNSRecord
	for _, rrset := range output.ResourceRecordSets {
		if aws.StringValue(rrset.Name) != fqdn || aws.StringValue(rrset.Type) != recordType {
			continue
		}

		ttl := int(aws.Int64Value(rrset.TTL))
		for _, rr := range rrset.ResourceRecords {
			records = append(records, parseRDataValue(
				aws.StringValue(rrset.Name),
				recordType,
				aws.StringValue(rr.Value),
				ttl,
			))
		}
	}

	return records, nil
}
