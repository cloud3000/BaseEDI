/*
Started as child process by ediserv
As a service, receives data from MMTS to create the XML MR Receipt file.
*/
package main

// MR_XML_RECEIPT for EDI service.
import (
	"encoding/xml"
	"fmt"
	"io/ioutil"
	"net"
	"net/smtp"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"gopkg.in/serveredi"

	"github.com/blackjack/syslog"
)

const emailfrom = "customer@cloud3000.com"
const emailto = "edimgr@cloud3000.com"

type credent struct {
	domain   string
	identity string
}

type repsattribute struct {
	attrval string
	name    string
}

type repspackage struct {
	pkgno          string
	pkgid          string
	parentpkgid    string
	action         string
	trackingno     string // MR-NO
	origtrackingno string
	packagetype    string
	pkgname        string
	carrier        string
	atpacker       string
	datepacked     string
	hazcode        string
	// Package unit of measure
	pkguomweight string
	pkguomlength string
	pkguomwidth  string
	pkguomheight string
	pkguomvolume string
	// Measured unit values
	pkgmeaweight string
	pkgmealength string
	pkgmeawidth  string
	pkgmeaheight string
	pkgmeavolume string
	//
	ordernumber    string
	projectnumber  string
	contractnumber string
}

type respline struct {
	lineNumber               string
	sublineNumber            string
	transactionquanity       string
	packlistquanity          string
	damagedquanity           string
	materialItemCode         string // 91G5999000378
	materialItemSize         string // 48x32x15
	materialType             string // >B<
	materialShortDescription string // >ASSEMBLY, LCD, 20X4, ALPH-NUM, W/ CBL</MaterialShortDescription>
	uom                      string // >EA</UnitOfMeasure>
	unitofmeasure            string // >EA</UnitOfMeasure>
	shippingQty              string // >1</ShippingQty>
	shippingUOM              string // >EAS</ShippingUOM>
	dateAtPacker             string //>05JAN17</DateAtPacker>
	IsAsset                  string
	assetNo                  string // </Asset>
	assetuid                 string
	SerialNumber             string
	Manufacture              string
	ModelNo                  string
	Sensitive                string
	ClientReportTable        string
	UIDSerialNumber          string
	UIDType                  string
}

func cepEmail(mailfrom string, mailto string, mailsub string, mailmsg string) int {
	// Set up authentication information.
	auth := smtp.PlainAuth("", "michael@cloud3000.com",
		"***passwd*****",
		"secure.emailsrvr.com")

	// Connect to the server, authenticate, set the sender and recipient,
	// and send the email all in one step.
	to := []string{mailto}
	msg := []byte("To: " + mailto + "\r\n" +
		"From: " + mailfrom + "\r\n" +
		"Subject: " + mailsub + "\r\n" +
		"\r\n" +
		mailmsg + "\r\n")
	err := smtp.SendMail("secure.emailsrvr.com:587", auth, "michael@cloud3000.com", to, msg)
	if err != nil {
		fmt.Printf("%v\n", err)
	}
	return 0
}

// MRresponse is the XML structure for the material receipt.
type MRresponse struct {
	message   string
	timestamp string
	version   string
	from      credent
	to        credent
	attr      []repsattribute
	mrpackage repspackage
	mrline    []respline
	Summary   struct {
		TotalLineItems string `xml:"TotalLineItems"`
		TotalPackages  string `xml:"TotalPackages"`
	} `xml:"Summary"`
}

func dateFromMMTS(mmtsdate string) string {
	yr := mmtsdate[0:2]
	mo := mmtsdate[2:4]
	da := mmtsdate[4:6]
	switch mo {
	case "01":
		return fmt.Sprintf("%s%s%s", da, "JAN", yr)
	case "02":
		return fmt.Sprintf("%s%s%s", da, "FEB", yr)
	case "03":
		return fmt.Sprintf("%s%s%s", da, "MAR", yr)
	case "04":
		return fmt.Sprintf("%s%s%s", da, "APR", yr)
	case "05":
		return fmt.Sprintf("%s%s%s", da, "MAY", yr)
	case "06":
		return fmt.Sprintf("%s%s%s", da, "JUN", yr)
	case "07":
		return fmt.Sprintf("%s%s%s", da, "JUL", yr)
	case "08":
		return fmt.Sprintf("%s%s%s", da, "AUG", yr)
	case "09":
		return fmt.Sprintf("%s%s%s", da, "SEP", yr)
	case "10":
		return fmt.Sprintf("%s%s%s", da, "OCT", yr)
	case "11":
		return fmt.Sprintf("%s%s%s", da, "NOV", yr)
	case "12":
		return fmt.Sprintf("%s%s%s", da, "DEC", yr)
	default:
		return fmt.Sprintf("%s%s%s", da, "???", yr)
	}
}

func xmlResponce(resp *MRresponse) {
	type packageuom struct {
		XMLName    xml.Name `xml:"PackageUOM"`
		UOM        string   `xml:"uom,attr"`
		PackageUOM string   `xml:",chardata"`
	}

	type attributes struct {
		XMLName   xml.Name `xml:"Attribute"`
		Name      string   `xml:"name,attr"`
		Attribute string   `xml:",chardata"`
	}

	type repsattribute struct {
		name    string
		attrval string
	}

	type unitofmeasure struct {
		XMLName       xml.Name `xml:"UnitOfMeasure"`
		Uom           string   `xml:",attr"`
		UnitOfMeasure string   `xml:",chardata"`
	}
	type lineasset struct {
		XMLName           xml.Name `xml:"Asset"`
		AssetNo           string   `xml:"assetNo,attr"`
		AssetUID          string   `xml:"assetUID,attr"`
		SerialNumber      string   `xml:",chardata"`
		Manufacture       string   `xml:",chardata"`
		ModelNo           string   `xml:",chardata"`
		Sensitive         string   `xml:",chardata"`
		UIDSerialNumber   string   `xml:",chardata"`
		ClientReportTable string   `xml:",chardata"`
		UIDType           string   `xml:",chardata"`
	}

	type lnstruct struct {
		LineNumber               string `xml:"lineNumber,attr"`
		SublineNumber            string `xml:"sublineNumber,attr"`
		Transactionquanity       string `xml:"transactionquanity,attr"`
		Packlistquanity          string `xml:"packlistquanity,attr"`
		Damagedquanity           string `xml:"damagedquanity,attr"`
		MaterialItemCode         string `xml:"MaterialItemCode"`
		MaterialItemSize         string `xml:"MaterialItemSize"`
		MaterialType             string `xml:"MaterialType"`
		ExternalSubItemNumber    string `xml:"ExternalSubItemNumber"`
		MaterialShortDescription string `xml:"MaterialShortDescription"`
		UnitOfMeasure            packageuom
		ShippingQty              string `xml:"ShippingQty"`
		ShippingUOM              string `xml:"ShippingUOM"`
		DateAtPacker             string `xml:"DateAtPacker"`
		Asset                    lineasset
	}

	type fXML struct {
		MessageID string `xml:"MessageID,attr"`
		Timestamp string `xml:"timestamp,attr"`
		Version   string `xml:"version,attr"`
		Header    struct {
			From struct {
				Credential struct {
					Domain   string `xml:"domain,attr"`
					Identity string `xml:"Identity"`
				} `xml:"Credential"`
			} `xml:"From"`
			To struct {
				Credential struct {
					Domain   string `xml:"domain,attr"`
					Identity string `xml:"Identity"`
				} `xml:"Credential"`
			} `xml:"To"`
			Attributes struct {
				Attribute []attributes
			}
		} `xml:"Header"`
		Package struct {
			PackageID              string `xml:"packageID,attr"`
			ParentpackageID        string `xml:"parentpackageID,attr"`
			Action                 string `xml:"action,attr"`
			PackageNumber          string `xml:"PackageNumber"`
			WebLink                string `xml:"WebLink"`
			SRN                    string `xml:"SRN"`
			TrackingNo             string `xml:"TrackingNo"`
			OriginalTrackingNo     string `xml:"OriginalTrackingNo"`
			PackageType            string `xml:"PackageType"`
			InvoiceNo              string `xml:"InvoiceNo"`
			PackageName            string `xml:"PackageName"`
			Carrier                string `xml:"Carrier"`
			CarrierDocumentNo      string `xml:"CarrierDocumentNo"`
			AtPacker               string `xml:"AtPacker"`
			DatePacked             string `xml:"DatePacked"`
			DepartureDate          string `xml:"DepartureDate"`
			DestinationArrivalDate string `xml:"DestinationArrivalDate"`
			DateCustoms            string `xml:"DateCustoms"`
			DateMisc1              string `xml:"DateMisc1"`
			DateMisc2              string `xml:"DateMisc2"`
			DateMisc3              string `xml:"DateMisc3"`
			SealNo                 string `xml:"SealNo"`
			UNHazard               struct {
				Unhazcode string `xml:"unhazcode,attr"`
			} `xml:"UNHazard"`
			PackageUOMWeight     packageuom
			PackageUOMLength     packageuom
			PackageUOMWidth      packageuom
			PackageUOMHeight     packageuom
			PackageUOMVolume     packageuom
			PackageMeasureWeight string `xml:"PackageMeasureWeight"`
			PackageMeasureLength string `xml:"PackageMeasureLength"`
			PackageMeasureWidth  string `xml:"PackageMeasureWidth"`
			PackageMeasureHeight string `xml:"PackageMeasureHeight"`
			PackageMeasureVolume string `xml:"PackageMeasureVolume"`

			Order struct {
				OrderNumber    string     `xml:"orderNumber,attr"`
				ProjectNumber  string     `xml:"ProjectNumber"`
				ContractNumber string     `xml:"ContractNumber"`
				Line           []lnstruct `xml:"Line"`
			} `xml:"Order"`
		} `xml:"Package"`
		Summary struct {
			TotalLineItems string `xml:"TotalLineItems"`
			TotalPackages  string `xml:"TotalPackages"`
		} `xml:"Summary"`
	}

	syslog.Syslog(syslog.LOG_INFO, "Building MR Response")
	var vol float32
	rdata := &fXML{}
	t := time.Now()
	rdata.MessageID = resp.message
	rdata.Timestamp = t.Format("2006-01-02T15:04:05")
	rdata.Version = "1.0"
	rdata.Header.From.Credential.Domain = resp.from.domain
	rdata.Header.From.Credential.Identity = resp.from.identity
	rdata.Header.To.Credential.Domain = resp.to.domain
	rdata.Header.To.Credential.Identity = resp.to.identity

	for rat := range resp.attr {
		rdata.Header.Attributes.Attribute = append(rdata.Header.Attributes.Attribute,
			attributes{
				Attribute: resp.attr[rat].attrval,
				Name:      resp.attr[rat].name,
			})
	}

	rdata.Package.Action = resp.mrpackage.action
	rdata.Package.PackageID = resp.mrpackage.pkgid
	rdata.Package.PackageNumber = resp.mrpackage.pkgno
	rdata.Package.ParentpackageID = resp.mrpackage.parentpkgid
	rdata.Package.TrackingNo = resp.mrpackage.trackingno
	rdata.Package.PackageType = resp.mrpackage.packagetype
	rdata.Package.PackageName = resp.mrpackage.pkgname
	rdata.Package.Carrier = resp.mrpackage.carrier
	rdata.Package.AtPacker = resp.mrpackage.atpacker
	rdata.Package.DatePacked = resp.mrpackage.datepacked
	rdata.Package.UNHazard.Unhazcode = resp.mrpackage.hazcode
	rdata.Package.PackageUOMWeight = packageuom{UOM: "LB"}
	rdata.Package.PackageUOMLength = packageuom{UOM: "IN"}
	rdata.Package.PackageUOMWidth = packageuom{UOM: "IN"}
	rdata.Package.PackageUOMHeight = packageuom{UOM: "IN"}
	rdata.Package.PackageUOMVolume = packageuom{UOM: "FT3"}
	rdata.Package.PackageMeasureWeight = strings.TrimSpace(resp.mrpackage.pkgmeaweight)
	rdata.Package.PackageMeasureLength = strings.TrimSpace(resp.mrpackage.pkgmealength)
	rdata.Package.PackageMeasureWidth = strings.TrimSpace(resp.mrpackage.pkgmeawidth)
	rdata.Package.PackageMeasureHeight = strings.TrimSpace(resp.mrpackage.pkgmeaheight)
	w, _ := strconv.ParseFloat(rdata.Package.PackageMeasureWidth, 64)
	l, _ := strconv.ParseFloat(rdata.Package.PackageMeasureLength, 64)
	h, _ := strconv.ParseFloat(rdata.Package.PackageMeasureHeight, 64)
	wft := float32(w / 12)
	lft := float32(l / 12)
	hft := float32(h / 12)
	fmt.Printf(" Width: %6.6f\n", wft)
	fmt.Printf("Length: %6.6f\n", lft)
	fmt.Printf("Height: %6.6f\n", hft)
	vol = float32(lft * wft * hft)
	fmt.Printf("Cu.ft. Volume: %6.6f\n", vol)
	rdata.Package.PackageMeasureVolume = fmt.Sprintf("%6.6f", vol)
	rdata.Package.Order.OrderNumber = resp.mrpackage.ordernumber
	rdata.Package.Order.ProjectNumber = resp.mrpackage.projectnumber
	rdata.Package.Order.ContractNumber = resp.mrpackage.contractnumber

	for lnidx := range resp.mrline {
		rdata.Package.Order.Line = append(rdata.Package.Order.Line,
			lnstruct{
				LineNumber:               resp.mrline[lnidx].lineNumber,
				Transactionquanity:       resp.mrline[lnidx].transactionquanity,
				SublineNumber:            "0",
				Packlistquanity:          resp.mrline[lnidx].packlistquanity,
				Damagedquanity:           resp.mrline[lnidx].damagedquanity,
				MaterialItemCode:         resp.mrline[lnidx].materialItemCode,
				MaterialItemSize:         resp.mrline[lnidx].materialItemSize,
				MaterialShortDescription: resp.mrline[lnidx].materialShortDescription,
				ShippingQty:              resp.mrline[lnidx].shippingQty,
				DateAtPacker:             resp.mrline[lnidx].dateAtPacker,
				UnitOfMeasure: packageuom{
					PackageUOM: resp.mrline[lnidx].unitofmeasure,
					UOM:        resp.mrline[lnidx].uom},
				MaterialType: resp.mrline[lnidx].materialType,
			})
	}
	rdata.Summary.TotalLineItems = resp.Summary.TotalLineItems
	rdata.Summary.TotalPackages = resp.Summary.TotalPackages
	//
	// Set Filename with full path
	cpath := "/home/edimgr/cepdamco128/out/"
	newfn := fmt.Sprintf("%scustomer_MR_%s_%s_RECEIPTS_%s.xml",
		cpath,
		resp.mrpackage.contractnumber,
		strings.Replace(resp.mrpackage.ordernumber, "/", "_", -1),
		t.Format("20060102150405"))

	rdata.MessageID = fmt.Sprintf("%s_%s_RECEIPTS_%s",
		resp.mrpackage.contractnumber,
		resp.mrpackage.ordernumber,
		t.Format("2006010215040"))

	if m, err2 := xml.MarshalIndent(rdata, "", "\t"); err2 != nil {
		efrom := emailfrom
		eto := emailto
		esub := "[EDI] MR Response Error: "
		emsg := fmt.Sprintf(
			"Transfer Filename: %s\n\n"+
				"        MR-PkgID#: %s \n"+
				"            Error: %s\n"+
				"        Date Time: %s\n",
			path.Base(newfn),
			resp.mrpackage.pkgid,
			fmt.Sprintf("xml.MarshalIndent FAILED:%s ", err2.Error()),
			time.Now().Format("2006-01-02 15:04:05"))
		cepEmail(efrom, eto, esub, emsg)
		os.Exit(1)
	} else {
		xmlheader := fmt.Sprintf("<?xml version=\"1.0\" encoding=\"ISO-8859-1\" ?>\n")
		m = append([]byte(xmlheader), m...)
		fmt.Printf("\n%s", newfn)
		//fmt.Printf("\n%s\n\n", m)
		ioerr := ioutil.WriteFile(newfn, []byte(fmt.Sprintf("%s\n\n\n", m)), 0644)
		if ioerr != nil {
			fmt.Printf("%v", ioerr)
			efrom := emailfrom
			eto := emailto
			esub := "[EDI] MR Response Error: "
			emsg := fmt.Sprintf(
				"Transfer Filename: %s\n\n"+
					"        MR-PkgID#: %s \n"+
					"            Error: %s\n"+
					"        Date Time: %s\n",
				path.Base(newfn),
				resp.mrpackage.pkgid,
				fmt.Sprintf("ioutil.WriteFile FAILED: %s ", err2.Error()),
				time.Now().Format("2006-01-02 15:04:05"))
			cepEmail(efrom, eto, esub, emsg)
		} else {
			efrom := emailfrom
			eto := emailto
			esub := fmt.Sprintf("[EDI] MR Response  PkgID: %s", resp.mrpackage.pkgid)
			emsg := fmt.Sprintf(
				"Transfer Filename: %s\n\n"+
					"        MR-PkgID#: %s \n"+
					"           Status: Response file created Successfully.\n"+
					"        Date Time: %s\n",
				path.Base(newfn),
				resp.mrpackage.pkgid,
				time.Now().Format("2006-01-02 15:04:05"))
			cepEmail(efrom, eto, esub, emsg)

		}
	}
}

func main() {
	// Sorry to keep you waiting, complicated business.

	syslog.Openlog("MR_XML_RECEIPT", syslog.LOG_PID, syslog.LOG_USER)
	syslog.Syslog(syslog.LOG_INFO, "MR Receipt started")
	defer syslog.Syslog(syslog.LOG_INFO, "MR Receipt ended")
	mrResp := &MRresponse{}

	conn, status := serveredi.Connect()
	if status.Number != 0 {
		errstr := fmt.Sprintf("%s Error=%d", status.Message, status.Number)
		fmt.Printf("%s ", errstr)
		efrom := emailfrom
		eto := emailto
		esub := "[EDI] MR_Receipt Network Error"
		emsg := fmt.Sprintf(
			"     Operation: %s\n"+
				"  Error Number: %d\n"+
				" Error Message: %s\n"+
				"     Date Time: %s\n",
			status.Op,
			status.Number,
			status.Message,
			time.Now().Format("2006-01-02 15:04:05"))
		cepEmail(efrom, eto, esub, emsg)
		os.Exit(1)
	}

	var locaddr net.Addr = conn.LocalAddr()
	var remaddr net.Addr = conn.RemoteAddr()
	fmt.Printf("MR %v received request from %v\n", locaddr, remaddr)
	syslog.Syslogf(syslog.LOG_INFO, "MR %v received request from %v", locaddr, remaddr)
	var received int
	mrResp.from.domain = "customer.com"
	mrResp.from.identity = "MaterialManager@customer.com"
	mrResp.to.domain = "customer.com"
	mrResp.to.identity = "MaterialManager@customer.com"
	mrResp.attr = append(mrResp.attr,
		repsattribute{
			name:    "SourceSystem",
			attrval: "MatMan",
		})
	mrResp.attr = append(mrResp.attr,
		repsattribute{
			name:    "SourceSystemVersion",
			attrval: " ",
		})

	mrResp.mrpackage.action = "Receipt"
	lineidx := 0
	// Now we start receiving datastr records from MMTS in this for loop
	for received = 0; ; received++ {
		datastr, status := serveredi.Recv(conn)
		if status.Number != 0 {
			fmt.Printf("MR Recv failed: %s\n", status.Message)
			errstr := fmt.Sprintf("%s Error=%d", status.Message, status.Number)
			fmt.Printf("%s ", errstr)
			efrom := emailfrom
			eto := emailto
			esub := "[EDI] MR_Receipt Network Error"
			emsg := fmt.Sprintf(
				"     Operation: %s\n"+
					"  Error Number: %d\n"+
					" Error Message: %s\n"+
					"     Date Time: %s\n",
				status.Op,
				status.Number,
				status.Message,
				time.Now().Format("2006-01-02 15:04:05"))
			cepEmail(efrom, eto, esub, emsg)
			os.Exit(1)
		}
		// MMTS will tell us when it's done sending data
		if datastr[0:status.Len] == "EDIEOF" {
			break
		}
		//fmt.Printf("%s\n", datastr[0:status.Len])

		// Data records from MMTS contain data item and data value
		// separated by '=', so we will split these in two.
		netvalSplit := strings.Split(datastr[0:status.Len], "=")
		if len(netvalSplit) != 2 {
			continue
		}
		//
		//		Based on data item name (netvalSplit[0]), we will start
		//    loading the XML structure with data values (netvalSplit[1]).
		//
		//		mrResp.mrpackage.parentpkgid = "CCMR1701057775"
		//		mrResp.mrpackage.trackingno = "CCMR1701057775"
		//if netvalSplit[0] == "PKGDETL-MR-NO" {
		//	mrResp.mrpackage.parentpkgid = netvalSplit[1]
		//	mrResp.mrpackage.trackingno = netvalSplit[1]
		//}

		//		mrResp.mrpackage.pkgid = "000001" //
		if netvalSplit[0] == "PKGDETL-PKG-NO" {
			mrResp.mrpackage.pkgid = netvalSplit[1]
			mrResp.mrpackage.trackingno = netvalSplit[1]
		}

		if netvalSplit[0] == "PKGDETL-PackageNumber" {
			mrResp.mrpackage.pkgno = netvalSplit[1]
		}

		//		mrResp.mrpackage.packagetype = "PALLET"
		//		mrResp.mrpackage.pkgname = "PALLET"
		if netvalSplit[0] == "PKG-DESCRIPTION" {
			mrResp.mrpackage.packagetype = netvalSplit[1]
			mrResp.mrpackage.pkgname = netvalSplit[1]
		}

		//		mrResp.mrpackage.carrier = "FEDEX"
		if netvalSplit[0] == "MRHEAD-CARRIER" {
			mrResp.mrpackage.carrier = netvalSplit[1]
		}

		//	mrResp.mrpackage.atpacker = "27JAN17"
		if netvalSplit[0] == "MRHEAD-DATE-RECV" {
			mrResp.mrpackage.atpacker = dateFromMMTS(netvalSplit[1])
		}

		// MRHEAD-UN-NO=199600
		if netvalSplit[0] == "MRHEAD-UN-NO" {
			haz := strings.Replace(netvalSplit[1], "000000", "", -1)
			if len(haz) > 1 {
				mrResp.mrpackage.hazcode = haz
			}
		}

		// POHEAD-REQ-NO=L414100153
		if netvalSplit[0] == "POHEAD-REQ-NO" {
			mrResp.mrpackage.projectnumber = netvalSplit[1]
		}

		// POHEAD-PROJECT-CODE=G41
		if netvalSplit[0] == "POHEAD-PROJECT-CODE" {
			mrResp.mrpackage.contractnumber = netvalSplit[1]
		}

		// MRHEAD-PO-NO=P2-G-H41-701052
		// mrResp.mrpackage.ordernumber = "P2-PC-H03-231100"
		if netvalSplit[0] == "MRHEAD-PO-NO" {
			mrResp.mrpackage.ordernumber = netvalSplit[1]
		}

		//PKGDETL-LENGTH
		if netvalSplit[0] == "PKGDETL-LENGTH" {
			mrResp.mrpackage.pkgmealength = netvalSplit[1]
		}
		//PKGDETL-WIDTH
		if netvalSplit[0] == "PKGDETL-WIDTH" {
			mrResp.mrpackage.pkgmeawidth = netvalSplit[1]
		}
		//PKGDETL-HEIGHT
		if netvalSplit[0] == "PKGDETL-HEIGHT" {
			mrResp.mrpackage.pkgmeaheight = netvalSplit[1]
		}
		//PKGDETL-TOT-LBS
		if netvalSplit[0] == "PKGDETL-TOT-LBS" {
			mrResp.mrpackage.pkgmeaweight = netvalSplit[1]
		}

		if netvalSplit[0] == "MRDETL-MR-ITEM-NO" {
			lineidx++
			mrResp.mrline = append(mrResp.mrline, respline{})
			mrResp.mrline[lineidx-1].dateAtPacker = mrResp.mrpackage.atpacker
			mrResp.mrline[lineidx-1].damagedquanity = "0"
		}

		// MRDETL-ITEM-REF=    1
		// mrResp.mrline[0].lineNumber = 1
		if netvalSplit[0] == "MRDETL-ITEM-REF" {
			mrResp.mrline[lineidx-1].lineNumber = strings.TrimSpace(netvalSplit[1])
		}

		// MRDETL-RECV-QTY=     1.00
		// mrResp.mrline[0].transactionquanity = 1
		if netvalSplit[0] == "MRDETL-RECV-QTY" {
			mrResp.mrline[lineidx-1].transactionquanity = strings.TrimSpace(netvalSplit[1])
			mrResp.mrline[lineidx-1].packlistquanity = strings.TrimSpace(netvalSplit[1])
			mrResp.mrline[lineidx-1].shippingQty = strings.TrimSpace(netvalSplit[1])
		}

		// PODETL-ITEMNO=91G5999000378
		if netvalSplit[0] == "PODETL-ITEMNO" {
			mrResp.mrline[lineidx-1].materialItemCode = netvalSplit[1]
		}

		// PODETL-ITEMNO-DESCR=ASSEMBLY, LCD, 20X4, ALPH-NUM, W/ CBLMAINSTREAM REEFER MRU
		if netvalSplit[0] == "PODETL-ITEMNO-DESCR" {
			mrResp.mrline[lineidx-1].materialShortDescription = netvalSplit[1]
		}
		if netvalSplit[0] == "PODETD-MaterialItemSize" {
			mrResp.mrline[lineidx-1].materialItemSize = netvalSplit[1]
		}
		// mrResp.mrline[1].materialType = "B"
		if netvalSplit[0] == "PODETD-MaterialType" {
			mrResp.mrline[lineidx-1].materialType = netvalSplit[1]
		}

		if netvalSplit[0] == "PODETL-UNIT-MEA" {
			mrResp.mrline[lineidx-1].unitofmeasure = netvalSplit[1]
			mrResp.mrline[lineidx-1].uom = netvalSplit[1][1:1]
		}

		if netvalSplit[0] == "PODETL-UOM" {
			mrResp.mrline[lineidx-1].uom = netvalSplit[1]
			if len(mrResp.mrline[lineidx-1].uom) < 1 {
				mrResp.mrline[lineidx-1].uom = mrResp.mrline[lineidx-1].unitofmeasure[1:1]
			}
		}
		//mrResp.mrline[lineidx-1].sublineNumber = "0"

		if netvalSplit[0] == "PODETL-IsAsset" {
			mrResp.mrline[lineidx-1].IsAsset = netvalSplit[1]
		}
		if netvalSplit[0] == "PODETL-assetNo" {
			mrResp.mrline[lineidx-1].assetNo = netvalSplit[1]
		}
		if netvalSplit[0] == "PODETL-assetUID" {
			mrResp.mrline[lineidx-1].assetuid = netvalSplit[1]
		}
		if netvalSplit[0] == "PODETL-SerialNumber" {
			mrResp.mrline[lineidx-1].SerialNumber = netvalSplit[1]
		}
		if netvalSplit[0] == "PODETL-Manufacture" {
			mrResp.mrline[lineidx-1].Manufacture = netvalSplit[1]
		}
		if netvalSplit[0] == "PODETL-ModelNo" {
			mrResp.mrline[lineidx-1].ModelNo = netvalSplit[1]
		}
		if netvalSplit[0] == "PODETL-Sensitive" {
			mrResp.mrline[lineidx-1].Sensitive = netvalSplit[1]
		}
		if netvalSplit[0] == "PODETL-ClientReportTable" {
			mrResp.mrline[lineidx-1].ClientReportTable = netvalSplit[1]
		}
		if netvalSplit[0] == "PODETL-UIDSerialNumber" {
			mrResp.mrline[lineidx-1].UIDSerialNumber = netvalSplit[1]
		}
		if netvalSplit[0] == "PODETL-UIDType" {
			mrResp.mrline[lineidx-1].UIDType = netvalSplit[1]
		}
	}
	fmt.Printf("%d Records Received\n", received)
	serveredi.Disconnect(conn)
	mrResp.Summary.TotalLineItems = fmt.Sprintf("%d", lineidx)
	mrResp.Summary.TotalPackages = "1"
	xmlResponce(mrResp)
}
