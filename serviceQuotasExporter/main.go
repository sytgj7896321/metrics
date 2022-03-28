package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/acm"
	"github.com/aws/aws-sdk-go-v2/service/cloudfront"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/aws-sdk-go-v2/service/servicequotas"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"
)

type allClients struct {
	s3Client             *s3.Client
	acmClients           []*acm.Client
	cloudFrontClient     *cloudfront.Client
	serviceQuotasClients []*servicequotas.Client
}

var (
	regionList = []string{
		//"af-south-1",
		//"ap-east-1",
		"ap-northeast-1",
		"ap-northeast-2",
		"ap-northeast-3",
		"ap-south-1",
		"ap-southeast-1",
		"ap-southeast-2",
		//"ap-southeast-3",
		"ca-central-1",
		"eu-central-1",
		"eu-north-1",
		//"eu-south-1",
		"eu-west-1",
		"eu-west-2",
		"eu-west-3",
		//"me-south-1",
		"sa-east-1",
		"us-east-1",
		"us-east-2",
		//"us-gov-east-1",
		//"us-gov-west-1",
		"us-west-1",
		"us-west-2",
	}
	usEast1  = findUsEast1Index(regionList)
	interval int
	port     int
	roleArn  string
)

func init() {
	flag.IntVar(&interval, "interval", 5, "time interval(minutes) of calling aws api to collect data")
	flag.IntVar(&port, "port", 2112, "listen port")
	flag.StringVar(&roleArn, "roleArn", "", "IAM roleArn of calling aws api")
}

func main() {
	flag.Parse()
	if roleArn == "" {
		roleArn = os.Getenv("AWS_ROLE_ARN")
	}

	prometheus.MustRegister(
		failCount,
		totalCount,
		s3CurrentVec,
		s3LimitedVec,
		acmCurrentVec,
		acmLimitedVec,
		cfDistributionsCurrentVec,
		cfDistributionsLimitedVec,
		cfOAICurrentVec,
		cfOAILimitedVec,
	)

	all := new(allClients)
	all.lifeCycleEngine()

	go func() {
		for {
			select {
			case <-time.After(59 * time.Minute):
				all.lifeCycleEngine()
			}
		}
	}()

	trigger(all)

	http.Handle("/metrics", promhttp.Handler())
	log.Println("Service Quotas Exporter Started")
	log.Fatalln(http.ListenAndServe(":"+strconv.Itoa(port), nil))
}

func (all *allClients) lifeCycleEngine() {
	defaultConfig, err := config.LoadDefaultConfig(context.TODO(), config.WithRegion("us-west-2"))
	if err != nil {
		log.Fatalf("failed to load SDK configuration, %v\n", err)
	}

	fmt.Println(defaultConfig.Credentials)

	stsSvc := sts.NewFromConfig(defaultConfig)
	credentials := stscreds.NewAssumeRoleProvider(stsSvc, roleArn)
	defaultConfig.Credentials = aws.NewCredentialsCache(credentials)

	all.serviceQuotasClients = make([]*servicequotas.Client, 0)
	for _, v := range regionList {
		cfg, err := config.LoadDefaultConfig(context.TODO(), config.WithRegion(v))
		if err != nil {
			log.Fatalf("failed to load SDK configuration, %v\n", err)
		}
		stsSvc := sts.NewFromConfig(cfg)
		credentials := stscreds.NewAssumeRoleProvider(stsSvc, roleArn)
		cfg.Credentials = aws.NewCredentialsCache(credentials)
		all.serviceQuotasClients = append(all.serviceQuotasClients, servicequotas.NewFromConfig(cfg))
	}

	all.s3Client = s3.NewFromConfig(defaultConfig)

	all.acmClients = make([]*acm.Client, 0)
	for _, v := range regionList {
		cfg, err := config.LoadDefaultConfig(context.TODO(), config.WithRegion(v))
		if err != nil {
			log.Fatalf("failed to load SDK configuration, %v\n", err)
		}
		stsSvc := sts.NewFromConfig(cfg)
		credentials := stscreds.NewAssumeRoleProvider(stsSvc, roleArn)
		cfg.Credentials = aws.NewCredentialsCache(credentials)
		all.acmClients = append(all.acmClients, acm.NewFromConfig(cfg))
	}

	all.cloudFrontClient = cloudfront.NewFromConfig(defaultConfig)
}

func trigger(all *allClients) {
	for _, v := range []string{
		"s3Buckets",
		"acmCertificates",
		"cloudfrontDistributions",
		"cloudfrontOAI",
	} {
		go func(v string) {
			for {
				switch v {
				case "s3Buckets":
					all.checkBuckets()
				case "acmCertificates":
					all.checkCertificates()
				case "cloudfrontDistributions":
					all.checkCloudFrontDistributions()
				case "cloudfrontOAI":
					all.checkCloudFrontOAI()
				}
				time.Sleep(time.Duration(interval) * time.Minute)
			}
		}(v)
	}
}

func findUsEast1Index(s []string) int {
	for i, v := range s {
		if v == "us-east-1" {
			return i
		}
	}
	return 13
}

func (all *allClients) checkBuckets() {
	var wg sync.WaitGroup
	var mux sync.Mutex
	buckets, err := all.s3Client.ListBuckets(context.TODO(), &s3Inout)
	totalCount.WithLabelValues("listBuckets").Inc()
	if errorHandle(err, "listBuckets") {
		return
	}

	for _, v := range regionList {
		s3Result[v] = 0
	}

	wg.Add(len(buckets.Buckets))

	for _, v := range buckets.Buckets {
		go func(v types.Bucket) {
			defer wg.Done()
			defer mux.Unlock()
			location, err := all.s3Client.GetBucketLocation(context.TODO(), &s3.GetBucketLocationInput{
				Bucket:              v.Name,
				ExpectedBucketOwner: nil,
			})
			mux.Lock()
			totalCount.WithLabelValues("getLocation").Inc()
			if errorHandle(err, "getLocation") {
				return
			}
			if string(location.LocationConstraint) == "" {
				s3Result["us-east-1"]++
			} else {
				s3Result[string(location.LocationConstraint)]++
			}
		}(v)
	}
	wg.Wait()

	for i, v := range regionList {
		quota, err := all.serviceQuotasClients[i].ListRequestedServiceQuotaChangeHistoryByQuota(context.TODO(), &serviceQuotasS3FilterChanged)
		totalCount.WithLabelValues("listQuotasHistory").Inc()
		if errorHandle(err, "listQuotasHistory") {
			continue
		}
		if len(quota.RequestedQuotas) != 0 {
			s3CurrentVec.WithLabelValues(v).Set(float64(s3Result[v]))
			s3LimitedVec.WithLabelValues(v).Set(*quota.RequestedQuotas[len(quota.RequestedQuotas)-1].DesiredValue)
		} else {
			quotaDefault, err := all.serviceQuotasClients[i].GetAWSDefaultServiceQuota(context.TODO(), &serviceQuotasS3FilterDefault)
			totalCount.WithLabelValues("getQuotasDefault").Inc()
			if errorHandle(err, "getQuotasDefault") {
				continue
			}
			s3CurrentVec.WithLabelValues(v).Set(float64(s3Result[v]))
			s3LimitedVec.WithLabelValues(v).Set(*quotaDefault.Quota.Value)
		}
	}
}

func (all *allClients) checkCertificates() {
	for i, client := range all.acmClients {
		certificates, err := client.ListCertificates(context.TODO(), &acmInput)
		totalCount.WithLabelValues("ListCertificates").Inc()
		if errorHandle(err, "ListCertificates") {
			continue
		}

		if regionList[i] == "eu-north-1" {
			acmCurrentVec.WithLabelValues(regionList[i]).Set(float64(len(certificates.CertificateSummaryList)))
			acmLimitedVec.WithLabelValues(regionList[i]).Set(float64(2500))
			continue
		}

		quota, err := all.serviceQuotasClients[i].ListRequestedServiceQuotaChangeHistoryByQuota(context.TODO(), &serviceQuotasACMFilterChanged)
		totalCount.WithLabelValues("listQuotasHistory").Inc()
		if errorHandle(err, "listQuotasHistory") {
			continue
		}
		if len(quota.RequestedQuotas) != 0 {
			acmCurrentVec.WithLabelValues(regionList[i]).Set(float64(len(certificates.CertificateSummaryList)))
			acmLimitedVec.WithLabelValues(regionList[i]).Set(*quota.RequestedQuotas[len(quota.RequestedQuotas)-1].DesiredValue)
		} else {
			quotaDefault, err := all.serviceQuotasClients[i].GetAWSDefaultServiceQuota(context.TODO(), &serviceQuotasACMFilterDefault)
			totalCount.WithLabelValues("getQuotasDefault").Inc()
			if errorHandle(err, "getQuotasDefault") {
				continue
			}
			acmCurrentVec.WithLabelValues(regionList[i]).Set(float64(len(certificates.CertificateSummaryList)))
			acmLimitedVec.WithLabelValues(regionList[i]).Set(*quotaDefault.Quota.Value)
		}
	}
}

func (all *allClients) checkCloudFrontDistributions() {
	distributions, err := all.cloudFrontClient.ListDistributions(context.TODO(), &cfInput)
	totalCount.WithLabelValues("listDistributions").Inc()
	if errorHandle(err, "listDistributions") {
		return
	}
	quota, err := all.serviceQuotasClients[usEast1].ListRequestedServiceQuotaChangeHistoryByQuota(context.TODO(), &serviceQuotasCloudFrontDistributionsFilterChanged)
	totalCount.WithLabelValues("listQuotasHistory").Inc()
	if errorHandle(err, "listQuotasHistory") {
		return
	}
	if len(quota.RequestedQuotas) != 0 {
		cfDistributionsCurrentVec.Set(float64(len(distributions.DistributionList.Items)))
		cfDistributionsLimitedVec.Set(*quota.RequestedQuotas[len(quota.RequestedQuotas)-1].DesiredValue)
	} else {
		quotaDefault, err := all.serviceQuotasClients[usEast1].GetAWSDefaultServiceQuota(context.TODO(), &serviceQuotasCloudFrontDistributionsFilterDefault)
		totalCount.WithLabelValues("getQuotasDefault").Inc()
		if errorHandle(err, "getQuotasDefault") {
			return
		}
		cfDistributionsCurrentVec.Set(float64(len(distributions.DistributionList.Items)))
		cfDistributionsLimitedVec.Set(*quotaDefault.Quota.Value)
	}
}

func (all *allClients) checkCloudFrontOAI() {
	identities, err := all.cloudFrontClient.ListCloudFrontOriginAccessIdentities(context.TODO(), &cfOAIInput)
	totalCount.WithLabelValues("listOAI").Inc()
	if errorHandle(err, "listOAI") {
		return
	}
	quota, err := all.serviceQuotasClients[usEast1].ListRequestedServiceQuotaChangeHistoryByQuota(context.TODO(), &serviceQuotasCloudFrontOAIFilterChanged)
	totalCount.WithLabelValues("listQuotasHistory").Inc()
	if errorHandle(err, "listQuotasHistory") {
		return
	}
	if len(quota.RequestedQuotas) != 0 {
		cfOAICurrentVec.Set(float64(len(identities.CloudFrontOriginAccessIdentityList.Items)))
		cfOAILimitedVec.Set(*quota.RequestedQuotas[len(quota.RequestedQuotas)-1].DesiredValue)
	} else {
		quotaDefault, err := all.serviceQuotasClients[usEast1].GetAWSDefaultServiceQuota(context.TODO(), &serviceQuotasCloudFrontOAIFilterDefault)
		totalCount.WithLabelValues("getQuotasDefault").Inc()
		if errorHandle(err, "getQuotasDefault") {
			return
		}
		cfOAICurrentVec.Set(float64(len(identities.CloudFrontOriginAccessIdentityList.Items)))
		cfOAILimitedVec.Set(*quotaDefault.Quota.Value)
	}
}

func errorHandle(err error, apiName string) bool {
	if err != nil {
		failCount.WithLabelValues(apiName).Inc()
		log.Println(err)
		return true
	}
	return false
}
