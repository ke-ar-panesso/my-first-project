package main

import (
	"bufio"
	"crypto/sha256"
	"fmt"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"golang.org/x/sys/windows"
	"net"
	"operclite-1/gui"
	"operclite-1/mod_gui"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

func main() {
	//Logica de interconexion
	var operclite fyne.App = app.New()
	clearScreen()
	if len(os.Args) < 4 {
		fmt.Println("Parametros: operclite.exe <ip> <port> <time interval for reports>")
		return
	}
    //Guardamos el intervalo de tiempo para enviarlo despues
	setReportInterval := strings.TrimSpace(os.Args[3])
    //Fase de conexion, serverSocket es el socket servidor de linux
    serverSocket, err := connectionPhase(strings.TrimSpace(os.Args[1]), strings.TrimSpace(os.Args[2]))
    if err != nil {
        fmt.Println(err)
        os.Exit(1)
    }
	//Recibir mensaje por defecto de el servidor para saber si la conexión es correcta o no
	if strings.TrimSpace(receiveKey(serverSocket)) == "Unauthorized" { //Conexion invalida
		gui.ErrorWindow(operclite)
		serverSocket.Close()
		os.Exit(0)
	}
	fmt.Println("\n--------------------------------------")
	fmt.Println("*        Operclite Client Login        *")
	fmt.Println("----------------------------------------")
	sendCredentialMsg(serverSocket, "Time: "+setReportInterval+"\n")
	fmt.Println("Intervalo enviado")
	if !loginProcess(serverSocket) {
        clearScreen()
        serverSocket.Close()
        gui.ErrorWindow(operclite)
		return
	}
    fmt.Println("OK, iniciando comunicacion principal...")
	clearScreen()
	mainWin, gui_access, report_access := gui.StartMainWindow(operclite)
	quitC := make(chan bool)
	//Logica principal de comunicación
	go recMsgMain(serverSocket, quitC, gui_access, report_access)
	gui_access.SendButton.OnTapped = func() {
		envMsgMain(serverSocket, quitC, gui_access)
	}
	go func() {
		<-quitC
		serverSocket.Close()
		fyne.Do(func() {
			gui.ByeWindow(operclite)
			(*mainWin).Close()
            os.Exit(0)
		})
	}()
	(*mainWin).SetIcon(resourceIconPng)
	(*mainWin).ShowAndRun()
}

//Fase de conexion, se retorna la variable Conn cuando la conexion TCP se logra
func connectionPhase(remoteIp string, remotePort string) (net.Conn, error) {
    dirTCP, errAddr := net.ResolveTCPAddr("tcp4", remoteIp + ":" + remotePort)
    if errAddr != nil {
       fmt.Println(errAddr)
       return nil, errAddr
    }
	serverSocket, errDial := net.DialTCP("tcp4", nil, dirTCP)
    if errDial != nil {
        fmt.Println(errDial)
        return nil, errDial
    }
    return serverSocket, nil
}

/*Proceso de login, se recibe por parametro el socket y la app con el fin de invocar una ventana de error en caso
de que el cliente quede bloqueado  (esto sucedera si el los intentos llegan a superar el 6)*/
func loginProcess(socket net.Conn) bool {
	for {
		recDefaultMsg(socket) //Recibe mensaje de ingresar usuario
		fmt.Print("Ingrese el usuario: ")
		reader := bufio.NewReader(os.Stdin)
		usercli, _ := reader.ReadString('\n')
		sendCredentialMsg(socket, usercli) //Envia la credencial

		recDefaultMsg(socket) //Recibe mensaje de ingresar contraseña
		fmt.Print("Ingrese la contraseña: ")
		hPasswd, err := readPassword()
		if err != nil {
			fmt.Println("Error:", err)
			os.Exit(2)
		}
		sendCredentialMsg(socket, hPasswd) //Envia la credencial
		tempKey := strings.TrimSpace(receiveKey(socket))
        str_counter, prefixFound := strings.CutPrefix(strings.TrimSpace(tempKey), "Freeze: ")
        if prefixFound {
           counter, err := strconv.Atoi(str_counter)
           if err != nil {
				fmt.Println("Error al transformar en entero: ", err)
				return false
			}
            fmt.Println("Muchos intentos, espera", counter, "segundos")
			time.Sleep(time.Duration(counter) * time.Second)
			continue
        }
        if tempKey == "Retry" {
            continue
        }
        return tempKey == "Login OK"
	}
}

// ----------Leer las contraseñas de manera correcta para Windows-------------//
func readPassword() (string, error) {
	h := windows.Handle(os.Stdin.Fd())
	var oldMode uint32
	if err := windows.GetConsoleMode(h, &oldMode); err != nil {
		return "", fmt.Errorf("no se pudo obtener el modo de consola: %v", err)
	}

	newMode := oldMode &^ windows.ENABLE_ECHO_INPUT
	if err := windows.SetConsoleMode(h, newMode); err != nil {
		return "", fmt.Errorf("no se pudo desactivar el eco: %v", err)
	}

	reader := bufio.NewReader(os.Stdin)
	passwd, err := reader.ReadString('\n')
	_ = windows.SetConsoleMode(h, oldMode)
	fmt.Println()
	if err != nil {
		return "", fmt.Errorf("error al leer la contraseña: %v", err)
	}
	passwd = strings.TrimSpace(passwd)
	hashTemp := sha256.Sum256([]byte(passwd))
	hPasswd := fmt.Sprintf("%x", hashTemp)
	return hPasswd + "\n", nil
}

//--------------------Funciones de comunicacion de manera concurrente (Incluye la GUI)--------------------//
func envMsgMain(socket net.Conn, channel chan bool, gui *gui.GUIComponents) {
	msgEnv := gui.UserEntry.Text
	if strings.TrimSpace(msgEnv) == "" {
		return
	}
	env := bufio.NewWriter(socket)
	env.WriteString(msgEnv + "\n")
	env.Flush()
	gui.AddMessage("Cliente: " + gui.UserEntry.Text)
	gui.UserEntry.SetText("")
	if msgEnv == "bye" {
		channel <- true
		return
	}
}

func recMsgMain(socket net.Conn, channel chan bool, guiC *gui.GUIComponents, reports *gui.ReportComponents) {
	reader := bufio.NewReader(socket)
	procIndex := 0
	for {
		socket.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		msg, err := reader.ReadString('\n')
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			fmt.Println("Conexión cerrada:", err)
			channel <- true
			return
		}
		if msg == "\r\n" || msg == "" {
			continue
		}
		fyne.Do(func() {
            updateGUI(&procIndex, msg, reports, guiC)
        })
	}
}

//Actualizar la GUI
func updateGUI(procIndex *int, msg string, reports *gui.ReportComponents, guiC *gui.GUIComponents) {
	var repPrefix, procPrefix bool
	msg, repPrefix = strings.CutPrefix(msg, "Report: ")
	msg, procPrefix = strings.CutPrefix(msg, "Process")
	switch {
	case repPrefix: //Estamos recibiendo un reporte
		cpu, ram, disk, ram_label, process_label, _ := mod_gui.ComponentValues(strings.TrimSpace(msg))
		reports.Cpu_Bar.SetValue(cpu / 100.0)
		reports.Ram_Bar.SetValue(ram / 100.0)
		reports.Disk_Bar.SetValue(disk / 100.0)
		reports.Ram_Label.SetText(ram_label + " (Memoria usada / Memoria Total) ")
		reports.Process_Label.SetText("Cantidad de procesos en ejecución: " + process_label)
	case procPrefix: //Estamos recibiendo procesos
		cleanMsg := strings.TrimSpace(msg)
		if *procIndex < len(reports.TopProcesses) {
			reports.TopProcesses[*procIndex].SetText(cleanMsg)
			*procIndex++
		}
		if *procIndex >= len(reports.TopProcesses) {
			*procIndex = 0
		}
	default:
		guiC.AddMessage("Server: " + strings.TrimSpace(msg)) //Añade una label cada que se llame a la función
	}
}
//----------------------------------------------------------------------------------------------------------//

// --------Llaves de comunicación entre los sockets-----------//
func receiveKey(socket net.Conn) string {
	msg, _ := bufio.NewReader(socket).ReadString('\n')
	fmt.Println("Server -->", msg)
	return msg
}
//------------------------------------------------------------//

// --------Funciones bloqueantes para el flujo del login------//
func recDefaultMsg(socket net.Conn) {
	msg, _ := bufio.NewReader(socket).ReadString('\n')
	fmt.Println("Server -->", msg)
}

func sendCredentialMsg(socket net.Conn, credential string) {
	env := bufio.NewWriter(socket)
	env.WriteString(credential)
	env.Flush()
}
//-------------------------------------------------------------//

// ------Limpiar pantalla Windows-------//
func clearScreen() {
	shell := exec.Command("cmd", "/c", "cls")
	shell.Stdout = os.Stdout
	shell.Run()
}
