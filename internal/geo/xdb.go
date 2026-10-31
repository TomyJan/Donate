package geo

import (
	"strings"
	"time"
)

func parseXDB(region, version string, legacy bool) (Location, error) {
	if region == "" {
		return Location{}, nil
	}
	fields := strings.Split(region, "|")
	if len(fields) != 5 {
		return Location{}, ErrUnavailable
	}
	for i := range fields {
		fields[i] = cleanField(fields[i])
	}
	p := Location{Country: fields[0], Province: fields[1], City: fields[2], ISP: fields[3], Source: "xdb", Version: version, QueriedAt: time.Now().UTC()}
	if legacy {
		p.Province, p.City, p.ISP = fields[2], fields[3], fields[4]
	} else if fields[4] == "CN" {
		p.Country = "中国"
	}
	p = normalize(p)
	if p.Country == "" {
		return p, ErrUnavailable
	}
	return p, nil
}

func cleanField(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 160 {
		return ""
	}
	switch strings.ToLower(s) {
	case "0", "-", "--", "n/a", "null", "unknown", "未知", "未知地区":
		return ""
	}
	return s
}

func normalize(p Location) Location {
	p.Country = cleanField(p.Country)
	p.Province = cleanField(p.Province)
	p.City = cleanField(p.City)
	p.District = cleanField(p.District)
	p.ISP = cleanField(p.ISP)
	if strings.EqualFold(p.Country, "china") || strings.EqualFold(p.Country, "cn") || p.Country == "中华人民共和国" {
		p.Country = "中国"
	}
	if p.Country == "中国" {
		if name, ok := provinceAliases[strings.ToLower(p.Province)]; ok {
			p.Province = name
		}
		switch p.Province {
		case "北京市", "天津市", "上海市", "重庆市":
			p.Province = strings.TrimSuffix(p.Province, "市")
		case "广西壮族自治区":
			p.Province = "广西"
		case "内蒙古自治区":
			p.Province = "内蒙古"
		case "西藏自治区":
			p.Province = "西藏"
		case "宁夏回族自治区":
			p.Province = "宁夏"
		case "新疆维吾尔自治区":
			p.Province = "新疆"
		case "香港特别行政区":
			p.Province = "香港"
		case "澳门特别行政区":
			p.Province = "澳门"
		}
	}
	return p
}

var provinceAliases = func() map[string]string {
	groups := []string{
		"北京|北京市|Beijing", "天津|天津市|Tianjin", "河北|河北省|Hebei", "山西|山西省|Shanxi",
		"内蒙古|内蒙古自治区|Inner Mongolia|Nei Mongol", "辽宁|辽宁省|Liaoning", "吉林|吉林省|Jilin", "黑龙江|黑龙江省|Heilongjiang",
		"上海|上海市|Shanghai", "江苏|江苏省|Jiangsu", "浙江|浙江省|Zhejiang", "安徽|安徽省|Anhui",
		"福建|福建省|Fujian", "江西|江西省|Jiangxi", "山东|山东省|Shandong", "河南|河南省|Henan",
		"湖北|湖北省|Hubei", "湖南|湖南省|Hunan", "广东|广东省|Guangdong", "广西|广西壮族自治区|Guangxi", "海南|海南省|Hainan",
		"重庆|重庆市|Chongqing", "四川|四川省|Sichuan", "贵州|贵州省|Guizhou", "云南|云南省|Yunnan", "西藏|西藏自治区|Tibet|Xizang",
		"陕西|陕西省|Shaanxi", "甘肃|甘肃省|Gansu", "青海|青海省|Qinghai", "宁夏|宁夏回族自治区|Ningxia", "新疆|新疆维吾尔自治区|Xinjiang",
		"香港|香港特别行政区|Hong Kong", "澳门|澳门特别行政区|Macau|Macao", "台湾|台湾省|Taiwan",
	}
	aliases := map[string]string{}
	for _, group := range groups {
		names := strings.Split(group, "|")
		for _, n := range names {
			aliases[strings.ToLower(n)] = names[0]
		}
	}
	return aliases
}()
