/*
File name: XML_PO_import.go
Is a Child process started by public_input_servic:

 1. Input from a XML file, named by parent on the command-line (Args).

 2. To parse and processes XML data, sending 'Fixed Length' data to
    a partner process on another host & port (192.168.1.240:30770)

 3. Send results as XML response back to customer, written to out folder.

*/
package main

// PO_XML_IMPORT for EDI service.
import (
	"bytes"
	"encoding/xml"
	"flag"
	"fmt"
	"io/ioutil"
	"net"
	"net/smtp"
	"os"
	"path"
	"strings"
	"time"

	"github.com/blackjack/syslog"

	"github.com/cloud3000/ioedi" // EDI Socket client lib
)

// POresponse Data for xmlResponse
type POresponse struct {
	MessageID string
	Timestamp string
	Version   string
	Order     struct {
		OrderNumber    string
		Action         string
		ProjectNumber  string
		ContractNumber string
		Response       string
	}
}

const outpath = "/home/edimgr/acmeship/out/"
const emailfrom = "acmeship@cloud3000.com"
const emailto = "edimgr@cloud3000.com"
const custid = "ACMESHIP"
const smtpuser = "michael@cloud3000.com"
const smtppass = "xcvsdfwer234"
const smtpserv = "smtpserv.com"
const smtpport = ":587"

func ediEmail(mailfrom string, mailto string, mailsub string, mailmsg string) int {
	// Set up authentication information.
	auth := smtp.PlainAuth("", smptuser, smtppass, smtpserv)

	// Connect to the server, authenticate, set the sender and recipient,
	// and send the email all in one step.
	to := []string{mailto}
	msg := []byte("To: " + mailto + "\r\n" +
		"From: " + mailfrom + "\r\n" +
		"Subject: " + mailsub + "\r\n" +
		"\r\n" +
		mailmsg + "\r\n")
	err := smtp.SendMail(smtpserv+smtpport, auth, smptuser, to, msg)
	if err != nil {
		fmt.Printf("%v\n", err)
	}
	return 0
}

// Some XML files contain invalid UTF-8 characters, we try to fix.
func xmlfix(b []byte) []byte {
	b = bytes.Replace(b, []byte("ISO-8859-1"), []byte("UTF-8"), 10)
	for _, c := range b {
		if (int16(c) > 126 || int16(c) < 32) &&
			(int16(c) != 9 && int16(c) != 10 && int16(c) != 13) {
			switch c {
			case 146:
				b = bytes.Replace(b, []byte("\x92"), []byte("&apos;"), -1)
			case 190:
				b = bytes.Replace(b, []byte("\xBE"), []byte("3/4"), -1)
			case 189:
				b = bytes.Replace(b, []byte("\xBD"), []byte("1/2"), -1)
			case 188:
				b = bytes.Replace(b, []byte("\xBC"), []byte("1/4"), -1)
			case 153:
				b = bytes.Replace(b, []byte("\x99"), []byte("(TM)"), -1)
			case 149:
				b = bytes.Replace(b, []byte("\x95"), []byte(" "), -1)
			}
		}
	}
	// fmt.Printf("%s\n", b)
	return b
}

func xmlResponse(resp POresponse, linkActions string, linkResponse string) {

	type fXML struct {
		MessageID string `xml:"MessageID,attr"`
		Timestamp string `xml:"timestamp,attr"`
		Version   string `xml:"version,attr"`
		Order     struct {
			OrderNumber    string `xml:"orderNumber,attr"`
			Action         string `xml:"action,attr"`
			ProjectNumber  string `xml:"ProjectNumber"`
			ContractNumber string `xml:"ContractNumber"`
			Response       string `xml:"Response"`
		} `xml:"Order"`
	}
	syslog.Syslog(syslog.LOG_INFO, "Building Response")
	syslog.Syslogf(syslog.LOG_INFO, "Response: %s %s ",
		resp.Order.OrderNumber, linkResponse)

	rdata := &fXML{}
	t := time.Now()
	rdata.MessageID = resp.MessageID
	rdata.Timestamp = t.Format("2006-01-02T15:04:05")
	rdata.Version = resp.Version
	rdata.Order.OrderNumber = resp.Order.OrderNumber
	rdata.Order.Action = linkActions // resp.Order.Action
	rdata.Order.ProjectNumber = resp.Order.ProjectNumber
	rdata.Order.ContractNumber = resp.Order.ContractNumber
	rdata.Order.Response = linkResponse // resp.Order.Response
	orderparts := strings.Split(rdata.Order.OrderNumber, "/")
	var newfn string
	switch len(orderparts) {
	case 1:
		newfn = fmt.Sprintf("%sRESPONSE_%s_%s_PO_RESPONSE_%s.xml",
			outpath, custid, rdata.Order.ProjectNumber, orderparts[0])
	case 2:
		newfn = fmt.Sprintf("%sRESPONSE_%s_%s_PO_RESPONSE_%s_%s.xml",
			outpath, custid, rdata.Order.ProjectNumber, orderparts[0], orderparts[1])
	default:
		newfn = fmt.Sprintf("%sRESPONSE_%s_%s_PO_RESPONSE_%s.xml",
			outpath, custid, rdata.Order.ProjectNumber, rdata.Order.OrderNumber)
	}

	if m, err2 := xml.MarshalIndent(rdata, "", "\t"); err2 != nil {
		panic("xml.MarshalIndent FAILED: " + err2.Error())
	} else {
		xmlheader := fmt.Sprintf("<?xml version=\"1.0\" encoding=\"ISO-8859-1\" ?>\n")
		m = append([]byte(xmlheader), m...)
		fmt.Printf("\nResponse filename: %s\n", newfn)
		syslog.Syslogf(syslog.LOG_INFO, "Response file: %s\n", newfn)
		ioerr := ioutil.WriteFile(newfn, []byte(fmt.Sprintf("%s\n\n\n", m)), 0644)
		if ioerr != nil {
			fmt.Printf("%v", ioerr)
			efrom := emailfrom
			eto := emailto
			esub := "[EDI] PO Response WriteFile FAILED "
			emsg := fmt.Sprintf(
				"        Filename: %s\n\n"+
					"           Order: %s\n"+
					"         Project: %s\n"+
					"   Import Status: %s\n"+
					" Response Failed: %s\n"+
					"       Date Time: %s\n",
				path.Base(os.Args[1]),
				rdata.Order.OrderNumber,
				rdata.Order.ProjectNumber,
				linkActions,
				fmt.Sprintf("ioutil.WriteFile FAILED: %s ", ioerr.Error()),
				time.Now().Format("2006-01-02 15:04:05"))
			ediEmail(efrom, eto, esub, emsg)
		} else {
			efrom := emailfrom
			eto := emailto
			esub := "[EDI] PO Import Status: " + linkActions
			emsg := fmt.Sprintf(
				"      Filename: %s\n\n"+
					"         Order: %s\n"+
					"       Project: %s\n"+
					"Status Message: %s\n"+
					"     Date Time: %s\n",
				path.Base(os.Args[1]),
				rdata.Order.OrderNumber,
				rdata.Order.ProjectNumber,
				linkResponse,
				time.Now().Format("2006-01-02 15:04:05"))
			ediEmail(efrom, eto, esub, emsg)
		}

		fmt.Printf("\n%s\n\n", m)
	}
}

// Query is here
type Query struct {
	File `xml:"fXML"`
}

// File is the inbound XML data.
type File struct {
	Msg         string `xml:"MessageID,attr"`
	Datetime    string `xml:"timestamp,attr"`
	Fileversion string `xml:"version,attr"`
	Credfrom    struct {
		ID string `xml:"Identity"`
		Dm string `xml:"domain,attr"`
	} `xml:"Header>From>Credential"`
	Credto struct {
		ID string `xml:"Identity"`
		Dm string `xml:"domain,attr"`
	} `xml:"Header>To>Credential"`
	Fileord struct {
		Ordno             string `xml:"orderNumber,attr"`
		Prjord            string `xml:"projectOrderNumber,attr"`
		Action            string `xml:"action,attr"`
		ProjectNumber     string `xml:"ProjectNumber"`
		ContractNumber    string `xml:"ContractNumber"`
		VendorName        string `xml:"Vendor>Name"`
		VendorAddress1    string `xml:"Vendor>Address>Address1"`
		VendorCity        string `xml:"Vendor>Address>City"`
		VendorState       string `xml:"Vendor>Address>State"`
		VendorPostalCode  string `xml:"Vendor>Address>PostalCode"`
		VendorCountry     string `xml:"Vendor>Address>Country"`
		VendorContactName string `xml:"Vendor>ContactName"`
		VendorTelephone   string `xml:"Vendor>Telephone"`
		IncoTerms         string `xml:"IncoTerms"`
		IncoLocation      string `xml:"IncoLocation"`
		PODescription     string `xml:"PurchaseOrderDescription"`
		Comments          string `xml:"Comments"`
		Lineitem          []Line `xml:"Line"`
	} `xml:"Order"`
	OrderRequestSummary struct {
		TotalLineItems string `xml:"TotalLineItems"`
		TotalAmount    string `xml:"TotalAmount"`
		TotalQuantity  string `xml:"TotalQuantity"`
	} `xml:"OrderRequestSummary"`
}

// Attributes Comments
type Attributes struct {
	Attribute string
}

// Attrnames Comments
type Attrnames struct {
	AttrName string `xml:"name,attr"`
}

// Line defines XML PO line items.
type Line struct {
	LineNumber               string `xml:"lineNumber,attr"`
	Qty                      string `xml:"quantity,attr"`
	RevisionNumber           string `xml:"RevisionNumber"`
	IssueDate                string `xml:"IssueDate"`
	MaterialItemCode         string `xml:"MaterialItemCode"`
	MaterialItemSize         string `xml:"MaterialItemSize"`
	MaterialShortDescription string `xml:"MaterialShortDescription"`
	UM                       struct {
		UOM      string `xml:"uom,attr"`
		UOMDescr string `xml:"uom_desc,attr"`
	} `xml:"UnitOfMeasure"`
	ProjectUnitPrice         string `xml:"ProjectUnitPrice"`
	ProjectCurrency          string `xml:"ProjectCurrency"`
	POUnitPrice              string `xml:"POUnitPrice"`
	POCurrency               string `xml:"POCurrency"`
	MaterialType             string `xml:"MaterialType"`
	IsAsset                  string `xml:"IsAsset"`
	IsUID                    string `xml:"IsUID"`
	MaterialLongDescription  string `xml:"MaterialLongDescription"`
	Destination              string `xml:"Destination"`
	DeliveryDate             string `xml:"DeliveryDate"`
	Comments                 string `xml:"Comments"`
	HarmonizedTariffCode     string `xml:"HarmonizedTariffCode"`
	HarmonizedTariffCodeDesc string `xml:"HarmonizedTariffCodeDesc"`
	Subline                  string `xml:"Subline"`
}

func checkErr(err error) {
	if err != nil {
		panic(err)
	}
}

func dataPrint(c *net.TCPConn, format string, data string) int {
	str1 := fmt.Sprintf(format, data)
	// fmt.Printf(str1)
	str2 := strings.Replace(str1, "\n", " ", -1)
	str3 := strings.Split(str2, "\t")
	str4 := strings.TrimSpace(str3[0])
	str5 := strings.TrimSpace(str3[1])
	str6 := str4 + "=" + str5
	status := ioedi.Send(c, str6)
	if status.Number != 0 {
		efrom := emailfrom
		eto := emailto
		esub := "[EDI] PO Import Network Error"
		emsg := fmt.Sprintf(
			"     Filename: %s\n\n"+
				"    Operation: %s\n"+
				" Error Number: %d\n"+
				"Error Message: %s\n"+
				"    Date Time: %s\n",
			path.Base(os.Args[1]),
			status.Op,
			status.Number,
			status.Message,
			time.Now().Format("2006-01-02 15:04:05"))
		ediEmail(efrom, eto, esub, emsg)
		os.Exit(1)

	}
	return 0
}

func outputData(q Query) {
	syslog.Syslogf(syslog.LOG_INFO, "Connecting to: %s", "192.168.1.240:30770")
	conn, edierr := ioedi.Connect("192.168.1.240:30770")
	if edierr.Number != 0 {
		errstr := fmt.Sprintf("%s Error=%d", edierr.Message, edierr.Number)
		fmt.Printf("%s ", errstr)
		efrom := emailfrom
		eto := emailto
		esub := "[EDI] PO Import Network Error"
		emsg := fmt.Sprintf(
			"      Filename: %s\n\n"+
				"         Order: %s\n"+
				"       Project: %s\n"+
				"     Operation: %s\n"+
				"  Error Number: %d\n"+
				" Error Message: %s\n"+
				"     Date Time: %s\n",
			path.Base(os.Args[1]),
			q.File.Fileord.Ordno,
			q.File.Fileord.ProjectNumber,
			edierr.Op,
			edierr.Number,
			edierr.Message,
			time.Now().Format("2006-01-02 15:04:05"))
		ediEmail(efrom, eto, esub, emsg)
		os.Exit(1)
	}

	//fmt.Printf("\n ****** Purchase Order ****** \n")
	dataPrint(conn, "Msg                \t%s\n", q.File.Msg)
	dataPrint(conn, "Datetime           \t%s\n", q.File.Datetime)
	dataPrint(conn, "Fileversion        \t%s\n", q.File.Fileversion)
	dataPrint(conn, "TotalLineItems     \t%s\n", q.OrderRequestSummary.TotalLineItems)
	dataPrint(conn, "TotalAmount        \t%s\n", q.OrderRequestSummary.TotalAmount)
	dataPrint(conn, "TotalQuantity      \t%s\n", q.OrderRequestSummary.TotalQuantity)
	//fmt.Printf("\n ******   Credentials  ****** \n")
	dataPrint(conn, "from.Id            \t%s\n", q.File.Credfrom.ID)
	dataPrint(conn, "from.Dm            \t%s\n", q.File.Credfrom.Dm)
	dataPrint(conn, "to.Id              \t%s\n", q.File.Credto.ID)
	dataPrint(conn, "to.Dm              \t%s\n", q.File.Credto.Dm)
	//fmt.Printf("\n ******      Order     ****** \n")
	syslog.Syslogf(syslog.LOG_INFO, "Order number %s", q.Fileord.Ordno)
	dataPrint(conn, "Ordno              \t%s\n", q.File.Fileord.Ordno)
	dataPrint(conn, "Prjord             \t%s\n", q.File.Fileord.Prjord)
	dataPrint(conn, "Action             \t%s\n", q.File.Fileord.Action)
	dataPrint(conn, "ContractNumber     \t%s\n", q.File.Fileord.ContractNumber)
	dataPrint(conn, "IncoTerms          \t%s\n", q.File.Fileord.IncoTerms)
	dataPrint(conn, "IncoLocation       \t%s\n", q.File.Fileord.IncoLocation)
	dataPrint(conn, "PODescription      \t%s\n", q.File.Fileord.PODescription)
	dataPrint(conn, "Comments           \t%s\n", q.File.Fileord.Comments)
	//fmt.Printf("\n ******      Vendor    ****** \n")
	dataPrint(conn, "VendorName         \t%s\n", q.File.Fileord.VendorName)
	dataPrint(conn, "VendorContactName  \t%s\n", q.File.Fileord.VendorContactName)
	dataPrint(conn, "VendorAddress1     \t%s\n", q.File.Fileord.VendorAddress1)
	dataPrint(conn, "VendorCity         \t%s\n", q.File.Fileord.VendorCity)
	dataPrint(conn, "VendorState        \t%s\n", q.File.Fileord.VendorState)
	dataPrint(conn, "VendorPostalCode   \t%s\n", q.File.Fileord.VendorPostalCode)
	//fmt.Printf("\n ******   Line Items  ****** \n")
	for _, item := range q.File.Fileord.Lineitem {
		//syslog.Syslogf(syslog.LOG_INFO, "LineNumber %s", item.LineNumber)
		dataPrint(conn, "LineNumber               \t%s\n", item.LineNumber)
		dataPrint(conn, "Qty                      \t%s\n", item.Qty)
		dataPrint(conn, "RevisionNumber           \t%s\n", item.RevisionNumber)
		dataPrint(conn, "IssueDate                \t%s\n", item.IssueDate)
		dataPrint(conn, "MaterialItemCode         \t%s\n", item.MaterialItemCode)
		dataPrint(conn, "MaterialItemSize         \t%s\n", item.MaterialItemSize)
		dataPrint(conn, "MaterialShortDescription \t%s\n", item.MaterialShortDescription)
		dataPrint(conn, "UOM                      \t%s\n", item.UM.UOM)
		dataPrint(conn, "UOMDescr                 \t%s\n", item.UM.UOMDescr)
		dataPrint(conn, "ProjectUnitPrice         \t%s\n", item.ProjectUnitPrice)
		dataPrint(conn, "ProjectCurrency          \t%s\n", item.ProjectCurrency)
		dataPrint(conn, "POUnitPrice              \t%s\n", item.POUnitPrice)
		dataPrint(conn, "POCurrency               \t%s\n", item.POCurrency)
		dataPrint(conn, "MaterialType             \t%s\n", item.MaterialType)
		dataPrint(conn, "IsAsset                  \t%s\n", item.IsAsset)
		dataPrint(conn, "IsUID                    \t%s\n", item.IsUID)
		dataPrint(conn, "MaterialLongDescription  \t%s\n", item.MaterialLongDescription)
		dataPrint(conn, "Destination              \t%s\n", item.Destination)
		dataPrint(conn, "DeliveryDate             \t%s\n", item.DeliveryDate)
		dataPrint(conn, "Comments                 \t%s\n", item.Comments)
		dataPrint(conn, "HarmonizedTariffCode     \t%s\n", item.HarmonizedTariffCode)
		dataPrint(conn, "HarmonizedTariffCodeDesc \t%s\n", item.HarmonizedTariffCodeDesc)
		dataPrint(conn, "Subline                  \t%s\n\n\n", item.Subline)
		// Send Asset placeholders for line item.
		if item.IsAsset == "Yes" {
			dataPrint(conn, "assetNo \t%s\n", " ")
			dataPrint(conn, "assetUID \t%s\n", " ")
			dataPrint(conn, "SerialNumber \t%s\n", " ")
			dataPrint(conn, "Manufacture \t%s\n", " ")
			dataPrint(conn, "ModelNo \t%s\n", " ")
			dataPrint(conn, "Sensitive \t%s\n", " ")
			dataPrint(conn, "ClientReportTable \t%s\n", " ")
			dataPrint(conn, "UIDSerialNumber \t%s\n", " ")
			dataPrint(conn, "UIDType \t%s\n", " ")
			efrom := emailfrom
			eto := emailto
			esub := "[EDI] Incoming Asset: " + q.Fileord.Ordno
			emsg := fmt.Sprintf(
				"              PO: %s\n"+
					"       Line item: %s\n"+
					"MaterialItemCode: %s\n"+
					"           Value: $%s %s\n"+
					"     DESCRIPTION: %s\n\n"+
					"       Date Time: %s\n",
				q.Fileord.Ordno,
				item.LineNumber,
				item.MaterialItemCode,
				item.POUnitPrice,
				item.POCurrency,
				item.MaterialShortDescription,
				time.Now().Format("2006-01-02 15:04:05"))
			ediEmail(efrom, eto, esub, emsg)
		}
	}
	ioedi.Send(conn, "EDIEOF")
	t := time.Now()
	var resp POresponse
	resp.MessageID = q.File.Msg
	resp.Timestamp = t.Format("2006-01-02T15:04:05")
	resp.Version = q.Fileversion
	resp.Order.OrderNumber = q.File.Fileord.Ordno
	resp.Order.ProjectNumber = q.File.Fileord.ProjectNumber
	resp.Order.ContractNumber = q.File.Fileord.ContractNumber
	myaction, actstat := ioedi.Recv(conn)
	myresponse, respstat := ioedi.Recv(conn)
	fmt.Printf("action len=%d\n", actstat.Len)
	fmt.Printf("ressp len=%d\n", respstat.Len)
	resp.Order.Action = fmt.Sprintf("%s", myaction[0:actstat.Len])
	resp.Order.Response = fmt.Sprintf("%s", myresponse[0:respstat.Len])
	syslog.Syslogf(syslog.LOG_INFO, "DisConnecting from: %s", "192.168.1.240:30770")
	ioedi.Disconnect(conn)
	xmlResponse(resp, resp.Order.Action, resp.Order.Response)

}

func main() {
	syslog.Openlog("XML_PO_import", syslog.LOG_PID, syslog.LOG_USER)
	syslog.Syslog(syslog.LOG_INFO, "XML_PO_import started")
	defer syslog.Syslog(syslog.LOG_INFO, "XML_PO_import ended")

	flag.Usage = func() {
		fmt.Printf("Usage of %s:", os.Args[0])
		fmt.Printf(" followed by One xml filename. \n")
		flag.PrintDefaults()
	}

	flag.Parse()
	if flag.NArg() == 0 {
		flag.Usage()
		os.Exit(1)
	}
	if flag.NArg() > 1 {
		flag.Usage()
		os.Exit(1)
	}

	for _, fn := range flag.Args() {
		syslog.Syslogf(syslog.LOG_ERR, "%s", fn)
		xmlFile, err := os.Open(fn)
		if err != nil {
			syslog.Syslogf(syslog.LOG_ERR, "%s", err.Error())
			panic(err)
		}
		fmt.Printf("\nFile: %s\n", fn)
		b, _ := ioutil.ReadAll(xmlFile)
		b = xmlfix(b)

		// Unmarshal the xml file.
		var q Query
		xmlerr := xml.Unmarshal(b, &q)
		if xmlerr != nil {
			fmt.Printf("%s\n", xmlerr.Error())
			syslog.Err(xmlerr.Error())
			var resp POresponse
			fileparts := strings.Split(os.Args[1], "_")
			t := time.Now()
			resp.MessageID = strings.Replace(path.Base(strings.Join(fileparts, "_")), ".xml", "", -1)
			resp.Timestamp = t.Format("2006-01-02T15:04:05")
			resp.Version = "1.0"
			resp.Order.OrderNumber = fileparts[3]
			resp.Order.ProjectNumber = fileparts[2]
			resp.Order.ContractNumber = fileparts[2]
			xmlResponse(resp, "ERROR", xmlerr.Error())
			os.Exit(1)
		}
		outputData(q)
		xmlFile.Close()
	}
}
