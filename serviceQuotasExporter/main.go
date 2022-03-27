package main

import (
	"context"
	"flag"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/acm"
	"github.com/aws/aws-sdk-go-v2/service/cloudfront"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/aws-sdk-go-v2/service/servicequotas"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"log"
	"net/http"
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
)

func init() {
	flag.IntVar(&interval, "interval", 5, "time interval(minutes) of call aws api to collect data")
	flag.IntVar(&port, "port", 2112, "listen port")
}

func main() {
	flag.Parse()
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

	defaultConfig, err := config.LoadDefaultConfig(context.TODO())
	if err != nil {
		log.Fatalf("failed to load SDK configuration, %v\n", err)
	}

	serviceQuotasClients := make([]*servicequotas.Client, 0)
	for _, v := range regionList {
		cfg, err := config.LoadDefaultConfig(context.TODO(), config.WithRegion(v))
		if err != nil {
			log.Fatalf("failed to load SDK configuration, %v\n", err)
		}
		serviceQuotasClients = append(serviceQuotasClients, servicequotas.NewFromConfig(cfg))
	}

	s3Client := s3.NewFromConfig(defaultConfig)

	acmClients := make([]*acm.Client, 0)
	for _, v := range regionList {
		cfg, err := config.LoadDefaultConfig(context.TODO(), config.WithRegion(v))
		if err != nil {
			log.Fatalf("failed to load SDK configuration, %v\n", err)
		}
		acmClients = append(acmClients, acm.NewFromConfig(cfg))
	}

	cloudFrontClient := cloudfront.NewFromConfig(defaultConfig)

	trigger(&allClients{
		s3Client:             s3Client,
		acmClients:           acmClients,
		cloudFrontClient:     cloudFrontClient,
		serviceQuotasClients: serviceQuotasClients,
	})

	http.Handle("/metrics", promhttp.Handler())
	log.Println("Service Quotas Exporter Started")
	log.Fatalln(http.ListenAndServe(":"+strconv.Itoa(port), nil))
}

func trigger(clients *allClients) {
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
					checkBuckets(clients)
				case "acmCertificates":
					checkCertificates(clients)
				case "cloudfrontDistributions":
					checkCloudFrontDistributions(clients)
				case "cloudfrontOAI":
					checkCloudFrontOAI(clients)
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

func checkBuckets(clients *allClients) {
	var wg sync.WaitGroup
	var mux sync.Mutex
	buckets, err := clients.s3Client.ListBuckets(context.TODO(), &s3Inout)
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
			location, err := clients.s3Client.GetBucketLocation(context.TODO(), &s3.GetBucketLocationInput{
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
		quota, err := clients.serviceQuotasClients[i].ListRequestedServiceQuotaChangeHistoryByQuota(context.TODO(), &serviceQuotasS3FilterChanged)
		totalCount.WithLabelValues("listQuotasHistory").Inc()
		if errorHandle(err, "listQuotasHistory") {
			continue
		}
		if len(quota.RequestedQuotas) != 0 {
			s3CurrentVec.WithLabelValues(v).Set(float64(s3Result[v]))
			s3LimitedVec.WithLabelValues(v).Set(*quota.RequestedQuotas[len(quota.RequestedQuotas)-1].DesiredValue)
		} else {
			quotaDefault, err := clients.serviceQuotasClients[i].GetAWSDefaultServiceQuota(context.TODO(), &serviceQuotasS3FilterDefault)
			totalCount.WithLabelValues("getQuotasDefault").Inc()
			if errorHandle(err, "getQuotasDefault") {
				continue
			}
			s3CurrentVec.WithLabelValues(v).Set(float64(s3Result[v]))
			s3LimitedVec.WithLabelValues(v).Set(*quotaDefault.Quota.Value)
		}
	}

}

func checkCertificates(clients *allClients) {
	for i, client := range clients.acmClients {
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

		quota, err := clients.serviceQuotasClients[i].ListRequestedServiceQuotaChangeHistoryByQuota(context.TODO(), &serviceQuotasACMFilterChanged)
		totalCount.WithLabelValues("listQuotasHistory").Inc()
		if errorHandle(err, "listQuotasHistory") {
			continue
		}
		if len(quota.RequestedQuotas) != 0 {
			acmCurrentVec.WithLabelValues(regionList[i]).Set(float64(len(certificates.CertificateSummaryList)))
			acmLimitedVec.WithLabelValues(regionList[i]).Set(*quota.RequestedQuotas[len(quota.RequestedQuotas)-1].DesiredValue)
		} else {
			quotaDefault, err := clients.serviceQuotasClients[i].GetAWSDefaultServiceQuota(context.TODO(), &serviceQuotasACMFilterDefault)
			totalCount.WithLabelValues("getQuotasDefault").Inc()
			if errorHandle(err, "getQuotasDefault") {
				continue
			}
			acmCurrentVec.WithLabelValues(regionList[i]).Set(float64(len(certificates.CertificateSummaryList)))
			acmLimitedVec.WithLabelValues(regionList[i]).Set(*quotaDefault.Quota.Value)
		}
	}
}

func checkCloudFrontDistributions(clients *allClients) {
	distributions, err := clients.cloudFrontClient.ListDistributions(context.TODO(), &cfInput)
	totalCount.WithLabelValues("listDistributions").Inc()
	if errorHandle(err, "listDistributions") {
		return
	}
	quota, err := clients.serviceQuotasClients[usEast1].ListRequestedServiceQuotaChangeHistoryByQuota(context.TODO(), &serviceQuotasCloudFrontDistributionsFilterChanged)
	totalCount.WithLabelValues("listQuotasHistory").Inc()
	if errorHandle(err, "listQuotasHistory") {
		return
	}
	if len(quota.RequestedQuotas) != 0 {
		cfDistributionsCurrentVec.Set(float64(len(distributions.DistributionList.Items)))
		cfDistributionsLimitedVec.Set(*quota.RequestedQuotas[len(quota.RequestedQuotas)-1].DesiredValue)
	} else {
		quotaDefault, err := clients.serviceQuotasClients[usEast1].GetAWSDefaultServiceQuota(context.TODO(), &serviceQuotasCloudFrontDistributionsFilterDefault)
		totalCount.WithLabelValues("getQuotasDefault").Inc()
		if errorHandle(err, "getQuotasDefault") {
			return
		}
		cfDistributionsCurrentVec.Set(float64(len(distributions.DistributionList.Items)))
		cfDistributionsLimitedVec.Set(*quotaDefault.Quota.Value)
	}
}

func checkCloudFrontOAI(clients *allClients) {
	identities, err := clients.cloudFrontClient.ListCloudFrontOriginAccessIdentities(context.TODO(), &cfOAIInput)
	totalCount.WithLabelValues("listOAI").Inc()
	if errorHandle(err, "listOAI") {
		return
	}
	quota, err := clients.serviceQuotasClients[usEast1].ListRequestedServiceQuotaChangeHistoryByQuota(context.TODO(), &serviceQuotasCloudFrontOAIFilterChanged)
	totalCount.WithLabelValues("listQuotasHistory").Inc()
	if errorHandle(err, "listQuotasHistory") {
		return
	}
	if len(quota.RequestedQuotas) != 0 {
		cfOAICurrentVec.Set(float64(len(identities.CloudFrontOriginAccessIdentityList.Items)))
		cfOAILimitedVec.Set(*quota.RequestedQuotas[len(quota.RequestedQuotas)-1].DesiredValue)
	} else {
		quotaDefault, err := clients.serviceQuotasClients[usEast1].GetAWSDefaultServiceQuota(context.TODO(), &serviceQuotasCloudFrontOAIFilterDefault)
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
