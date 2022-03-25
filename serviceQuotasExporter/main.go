package main

import (
	"context"
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

	usEast1 = findUsEast1Index(regionList)
)

func main() {
	prometheus.MustRegister(
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
	log.Fatalln(http.ListenAndServe(":2112", nil))
}

func trigger(clients *allClients) {
	for _, v := range []string{"s3", "acm", "cloudfront"} {
		go func(v string) {
			for {
				switch v {
				case "s3":
					checkBuckets(clients)
				case "acm":
					checkCertificates(clients)
				case "cloudfront":
					checkCloudFront(clients)
				}
				time.Sleep(5 * time.Minute)
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
	if err != nil {
		log.Fatalln(err)
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
			if err != nil {
				log.Fatalln(err)
			}
			mux.Lock()
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
		if err != nil {
			log.Fatalln(err)
		}
		if len(quota.RequestedQuotas) != 0 {
			s3CurrentVec.WithLabelValues(v).Set(float64(s3Result[v]))
			s3LimitedVec.WithLabelValues(v).Set(*quota.RequestedQuotas[len(quota.RequestedQuotas)-1].DesiredValue)
		} else {
			quotaDefault, err := clients.serviceQuotasClients[i].GetAWSDefaultServiceQuota(context.TODO(), &serviceQuotasS3FilterDefault)
			if err != nil {
				log.Fatalln(err)
			}
			s3CurrentVec.WithLabelValues(v).Set(float64(s3Result[v]))
			s3LimitedVec.WithLabelValues(v).Set(*quotaDefault.Quota.Value)
		}
	}

}

func checkCertificates(clients *allClients) {
	for i, client := range clients.acmClients {
		certificates, err := client.ListCertificates(context.TODO(), &acmInput)
		if err != nil {
			log.Fatalln(err)
		}

		if regionList[i] == "eu-north-1" {
			acmCurrentVec.WithLabelValues(regionList[i]).Set(float64(len(certificates.CertificateSummaryList)))
			acmLimitedVec.WithLabelValues(regionList[i]).Set(float64(2500))
			continue
		}

		quota, err := clients.serviceQuotasClients[i].ListRequestedServiceQuotaChangeHistoryByQuota(context.TODO(), &serviceQuotasACMFilterChanged)
		if err != nil {
			log.Fatalln(err)
		}
		if len(quota.RequestedQuotas) != 0 {
			acmCurrentVec.WithLabelValues(regionList[i]).Set(float64(len(certificates.CertificateSummaryList)))
			acmLimitedVec.WithLabelValues(regionList[i]).Set(*quota.RequestedQuotas[len(quota.RequestedQuotas)-1].DesiredValue)
		} else {
			quotaDefault, err := clients.serviceQuotasClients[i].GetAWSDefaultServiceQuota(context.TODO(), &serviceQuotasACMFilterDefault)
			if err != nil {
				log.Fatalln(err)
			}
			acmCurrentVec.WithLabelValues(regionList[i]).Set(float64(len(certificates.CertificateSummaryList)))
			acmLimitedVec.WithLabelValues(regionList[i]).Set(*quotaDefault.Quota.Value)
		}
	}
}

func checkCloudFront(clients *allClients) {
	distributions, err := clients.cloudFrontClient.ListDistributions(context.TODO(), &cfInput)
	if err != nil {
		log.Fatalln(err)
	}

	quota1, err := clients.serviceQuotasClients[usEast1].
		ListRequestedServiceQuotaChangeHistoryByQuota(context.TODO(), &serviceQuotasCloudFrontDistributionsFilterChanged)
	if err != nil {
		log.Fatalln(err)
	}
	if len(quota1.RequestedQuotas) != 0 {
		cfDistributionsCurrentVec.Set(float64(len(distributions.DistributionList.Items)))
		cfDistributionsLimitedVec.Set(*quota1.RequestedQuotas[len(quota1.RequestedQuotas)-1].DesiredValue)
	} else {
		quotaDefault1, err := clients.serviceQuotasClients[usEast1].GetAWSDefaultServiceQuota(context.TODO(), &serviceQuotasCloudFrontDistributionsFilterDefault)
		if err != nil {
			log.Fatalln(err)
		}
		cfDistributionsCurrentVec.Set(float64(len(distributions.DistributionList.Items)))
		cfDistributionsLimitedVec.Set(*quotaDefault1.Quota.Value)
	}

	identities, err := clients.cloudFrontClient.ListCloudFrontOriginAccessIdentities(context.TODO(), &cfOAIInput)
	if err != nil {
		log.Fatalln(err)
	}

	quota2, err := clients.serviceQuotasClients[usEast1].ListRequestedServiceQuotaChangeHistoryByQuota(context.TODO(), &serviceQuotasCloudFrontOAIFilterChanged)
	if err != nil {
		log.Fatalln(err)
	}
	if len(quota2.RequestedQuotas) != 0 {
		cfOAICurrentVec.Set(float64(len(identities.CloudFrontOriginAccessIdentityList.Items)))
		cfOAILimitedVec.Set(*quota2.RequestedQuotas[len(quota2.RequestedQuotas)-1].DesiredValue)
	} else {
		quotaDefault2, err := clients.serviceQuotasClients[usEast1].GetAWSDefaultServiceQuota(context.TODO(), &serviceQuotasCloudFrontOAIFilterDefault)
		if err != nil {
			log.Fatalln(err)
		}
		cfOAICurrentVec.Set(float64(len(identities.CloudFrontOriginAccessIdentityList.Items)))
		cfOAILimitedVec.Set(*quotaDefault2.Quota.Value)
	}
}
