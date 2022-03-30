package main

import (
	"context"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/acm"
	"github.com/aws/aws-sdk-go-v2/service/cloudfront"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/aws-sdk-go-v2/service/servicequotas"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spf13/viper"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"
)

type allClients struct {
	account              string
	key                  string
	secret               string
	defaultConfig        aws.Config
	perRegionConfig      []aws.Config
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
	account  []string
	key      []string
	secret   []string
)

func main() {
	viper.AddConfigPath("./")
	err := viper.ReadInConfig()
	if err != nil {
		log.Fatalln(err)
	}
	viper.SetDefault("service.interval", 5)
	viper.SetDefault("service.port", 2112)
	interval = viper.GetInt("service.interval")
	port = viper.GetInt("service.port")
	account = viper.GetStringSlice("info.account")
	key = viper.GetStringSlice("info.key")
	secret = viper.GetStringSlice("info.secret")

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

	for i := range account {
		go multiAccountControlEngine(i)
	}

	http.Handle("/metrics", promhttp.Handler())
	log.Println("Service Quotas Exporter Started")
	log.Fatalln(http.ListenAndServe(":"+strconv.Itoa(port), nil))
}

func multiAccountControlEngine(i int) {
	all := new(allClients)
	all.account = account[i]
	all.key = key[i]
	all.secret = secret[i]
	all.loadClientConfig()
	all.createClients()

	go func() {
		go trigger(all)
		for {
			select {
			case <-time.Tick(time.Duration(interval) * time.Minute):
				go trigger(all)
			}
		}
	}()
}

func (all *allClients) loadClientConfig() {
	all.defaultConfig = aws.Config{
		Region:      "us-west-2",
		Credentials: credentials.NewStaticCredentialsProvider(all.key, all.secret, ""),
	}

	for _, v := range regionList {
		cfg := aws.Config{
			Region:      v,
			Credentials: credentials.NewStaticCredentialsProvider(all.key, all.secret, ""),
		}
		all.perRegionConfig = append(all.perRegionConfig, cfg)
	}
}

func (all *allClients) createClients() {
	all.serviceQuotasClients = make([]*servicequotas.Client, 0)
	for _, v := range all.perRegionConfig {
		all.serviceQuotasClients = append(all.serviceQuotasClients, servicequotas.NewFromConfig(v))
	}

	all.s3Client = s3.NewFromConfig(all.defaultConfig)

	all.acmClients = make([]*acm.Client, 0)
	for _, v := range all.perRegionConfig {
		all.acmClients = append(all.acmClients, acm.NewFromConfig(v))
	}

	all.cloudFrontClient = cloudfront.NewFromConfig(all.defaultConfig)
}

func trigger(all *allClients) {
	go all.checkBuckets()
	go all.checkCertificates()
	go all.checkCloudFrontDistributions()
	go all.checkCloudFrontOAI()
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
	s3Result := make(map[string]int)
	buckets, err := all.s3Client.ListBuckets(context.TODO(), &s3Inout)
	totalCount.WithLabelValues("listBuckets").Inc()
	if errorHandle(err, "listBuckets") {
		return
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
			s3CurrentVec.WithLabelValues(all.account, v).Set(float64(s3Result[v]))
			s3LimitedVec.WithLabelValues(all.account, v).Set(*quota.RequestedQuotas[len(quota.RequestedQuotas)-1].DesiredValue)
		} else {
			quotaDefault, err := all.serviceQuotasClients[i].GetAWSDefaultServiceQuota(context.TODO(), &serviceQuotasS3FilterDefault)
			totalCount.WithLabelValues("getQuotasDefault").Inc()
			if errorHandle(err, "getQuotasDefault") {
				continue
			}
			s3CurrentVec.WithLabelValues(all.account, v).Set(float64(s3Result[v]))
			s3LimitedVec.WithLabelValues(all.account, v).Set(*quotaDefault.Quota.Value)
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
			acmCurrentVec.WithLabelValues(all.account, regionList[i]).Set(float64(len(certificates.CertificateSummaryList)))
			acmLimitedVec.WithLabelValues(all.account, regionList[i]).Set(float64(2500))
			continue
		}

		quota, err := all.serviceQuotasClients[i].ListRequestedServiceQuotaChangeHistoryByQuota(context.TODO(), &serviceQuotasACMFilterChanged)
		totalCount.WithLabelValues("listQuotasHistory").Inc()
		if errorHandle(err, "listQuotasHistory") {
			continue
		}
		if len(quota.RequestedQuotas) != 0 {
			acmCurrentVec.WithLabelValues(all.account, regionList[i]).Set(float64(len(certificates.CertificateSummaryList)))
			acmLimitedVec.WithLabelValues(all.account, regionList[i]).Set(*quota.RequestedQuotas[len(quota.RequestedQuotas)-1].DesiredValue)
		} else {
			quotaDefault, err := all.serviceQuotasClients[i].GetAWSDefaultServiceQuota(context.TODO(), &serviceQuotasACMFilterDefault)
			totalCount.WithLabelValues("getQuotasDefault").Inc()
			if errorHandle(err, "getQuotasDefault") {
				continue
			}
			acmCurrentVec.WithLabelValues(all.account, regionList[i]).Set(float64(len(certificates.CertificateSummaryList)))
			acmLimitedVec.WithLabelValues(all.account, regionList[i]).Set(*quotaDefault.Quota.Value)
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
		cfDistributionsCurrentVec.WithLabelValues(all.account).Set(float64(len(distributions.DistributionList.Items)))
		cfDistributionsLimitedVec.WithLabelValues(all.account).Set(*quota.RequestedQuotas[len(quota.RequestedQuotas)-1].DesiredValue)
	} else {
		quotaDefault, err := all.serviceQuotasClients[usEast1].GetAWSDefaultServiceQuota(context.TODO(), &serviceQuotasCloudFrontDistributionsFilterDefault)
		totalCount.WithLabelValues("getQuotasDefault").Inc()
		if errorHandle(err, "getQuotasDefault") {
			return
		}
		cfDistributionsCurrentVec.WithLabelValues(all.account).Set(float64(len(distributions.DistributionList.Items)))
		cfDistributionsLimitedVec.WithLabelValues(all.account).Set(*quotaDefault.Quota.Value)
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
		cfOAICurrentVec.WithLabelValues(all.account).Set(float64(len(identities.CloudFrontOriginAccessIdentityList.Items)))
		cfOAILimitedVec.WithLabelValues(all.account).Set(*quota.RequestedQuotas[len(quota.RequestedQuotas)-1].DesiredValue)
	} else {
		quotaDefault, err := all.serviceQuotasClients[usEast1].GetAWSDefaultServiceQuota(context.TODO(), &serviceQuotasCloudFrontOAIFilterDefault)
		totalCount.WithLabelValues("getQuotasDefault").Inc()
		if errorHandle(err, "getQuotasDefault") {
			return
		}
		cfOAICurrentVec.WithLabelValues(all.account).Set(float64(len(identities.CloudFrontOriginAccessIdentityList.Items)))
		cfOAILimitedVec.WithLabelValues(all.account).Set(*quotaDefault.Quota.Value)
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
